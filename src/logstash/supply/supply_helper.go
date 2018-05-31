package supply

import (
	"bufio"
	"fmt"
	"github.com/andibrunner/libbuildpack"
	"io"
	"io/ioutil"
	"logstash/util"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func (gs *Supplier) BPDir() string {
	return gs.BuildpackDir
}

func (gs *Supplier) NewDependency(name string, versionParts int, configVersion string, doCompile bool) (Dependency, error) {
	var dependency = Dependency{Name: name, VersionParts: versionParts, ConfigVersion: configVersion}

	if parsedVersion, err := gs.SelectDependencyVersion(dependency); err != nil {
		gs.Log.Error("Unable to determine the version of %s: %s", dependency, err.Error())
		return dependency, err
	} else {
		dependency.Version = parsedVersion
		dependency.DoCompile = doCompile
		dependency.FullName = dependency.Name + "-" + dependency.Version
		dependency.RuntimeLocation = gs.EvalRuntimeLocation(dependency)
		dependency.StagingLocation = gs.EvalStagingLocation(dependency)
		dependency.CacheLocation = gs.EvalCacheLocation(dependency)
		dependency.TmpLocation = gs.EvalTmpLocation(dependency)
		dependency.TmpExtractLocation = gs.EvalTmpExtractLocation(dependency)
	}

	return dependency, nil
}

func (gs *Supplier) WriteDependencyProfileD(dependencyName string, content string) error {

	if err := gs.Stager.WriteProfileD(dependencyName+".sh", content); err != nil {
		gs.Log.Error("Error writing profile.d script for %s: %s", dependencyName, err.Error())
		return err
	}
	return nil
}

func (gs *Supplier) ReadCachedDependencies() error {

	//clean up Cache
	if gs.LogstashConfig.Buildpack.NoCache {
		util.RemoveAllContents(gs.Stager.CacheDir()) // rm -r cache_dir/*
	} else {
		//if dependency dir with current supplier version doesn't exist delete all content in cache
		// cache_dir/dependencies/[supplier_version]/
		parentDir := filepath.Dir(gs.DepCacheDir)
		if _, err := os.Stat(parentDir); os.IsNotExist(err) {
			util.RemoveAllContents(gs.Stager.CacheDir()) // rm -r cache_dir/*
		}
	}
	gs.CachedDeps = make(map[string]string)
	os.MkdirAll(gs.DepCacheDir, 0755)

	cacheDir, err := ioutil.ReadDir(gs.DepCacheDir)
	if err != nil {
		gs.Log.Error("  --> failed reading cache directory: %s", err)
		return err
	}

	for _, dirEntry := range cacheDir {
		gs.Log.Debug(fmt.Sprintf("--> added dependency '%s' to cache list", dirEntry.Name()))
		gs.CachedDeps[dirEntry.Name()] = ""
	}

	return nil
}

func (gs *Supplier) InstallDependency(dependency Dependency) error {
	var err error

	//check if there are other cached versions of the same dependency
	for cachedDep := range gs.CachedDeps {
		if strings.HasPrefix(cachedDep, dependency.Name+"-") && cachedDep != dependency.FullName {
			gs.Log.Debug(fmt.Sprintf("--> deleting unused dependency version '%s' from application cache", cachedDep))
			gs.CachedDeps[cachedDep] = "deleted"
			os.RemoveAll(filepath.Join(gs.DepCacheDir, cachedDep))
		}
	}

	//check cache dir
	cacheDir := filepath.Dir(dependency.CacheLocation)
	if _, err := os.Stat(cacheDir); os.IsNotExist(err) {
		return err
	}

	//set up manifest dependency
	manifestDependency := libbuildpack.Dependency{Name: dependency.Name, Version: dependency.Version}

	entry, err := gs.Manifest.GetEntry(manifestDependency)
	if err != nil {
		return err
	}

	if _, err := os.Stat(dependency.CacheLocation); os.IsNotExist(err) { //not cached

		tarball := dependency.TmpLocation

		gs.Log.BeginStep("Installing %s %s", manifestDependency.Name, manifestDependency.Version)
		err = gs.Manifest.FetchDependency(manifestDependency, tarball)
		if err != nil {
			return err
		}

		err = gs.Manifest.WarnNewerPatch(manifestDependency)
		if err != nil {
			return err
		}

		err = gs.Manifest.WarnEndOfLife(manifestDependency)
		if err != nil {
			return err
		}

		//extract tarball
		extractLocation := dependency.CacheLocation
		if dependency.DoCompile {
			extractLocation = dependency.TmpExtractLocation
		}

		err = os.MkdirAll(extractLocation, 0755)
		if err != nil {
			return err
		}

		if strings.HasSuffix(entry.URI, ".zip") {
			err = libbuildpack.ExtractZip(tarball, extractLocation)
		} else if strings.HasSuffix(entry.URI, ".tar.gz") {
			err = libbuildpack.ExtractTarGz(tarball, extractLocation)
		} else {
			err = os.Rename(tarball, extractLocation)
		}

		if err != nil {
			gs.Log.Error("Error extracting '%s': %s", dependency.Name, err.Error())
			return err
		}

		//compile dependency
		if dependency.DoCompile {
			gs.CompileDependency(dependency, filepath.Join(extractLocation, dependency.Name), dependency.CacheLocation)
		}

	} else { //cached
		gs.Log.BeginStep("Installing %s %s from application cache", manifestDependency.Name, manifestDependency.Version)
	}

	// copy from cache to Stage
	err = gs.CopyToStage(dependency)
	if err != nil {
		gs.Log.Error("Error copying '%s' to stage: %s", dependency.Name, err.Error())
		return err
	}

	//remove cache if defined
	if gs.LogstashConfig.Buildpack.NoCache {
		os.RemoveAll(filepath.Join(gs.DepCacheDir, dependency.FullName))
	}

	//register dependency
	gs.CachedDeps[dependency.FullName] = "in use"

	return nil
}

func (gs *Supplier) CopyToStage(dep Dependency) error {
	out, err := exec.Command("cp", "-r", dep.CacheLocation+"/.", dep.StagingLocation).Output()
	if err != nil {
		gs.Log.Error(string(out))
		return err
	}
	return nil
}

func (gs *Supplier) LsDir(dir string) error {
	out, err := exec.Command("ls", "-al", dir).Output()
	if err != nil {
		gs.Log.Error(string(out))
		return err
	}
	gs.Log.Info(string(out))
	return nil
}

func (gs *Supplier) CompileDependency(dep Dependency, makeDir string, prefix string) error {

	//configure
	gs.Log.BeginStep("Starting Compilation of %s (Compilation only needs to be done the first time)", dep.FullName)

	gs.Log.Info("Step 1 of 3: configure ...")
	cmd := exec.Command("/bin/sh", filepath.Join(makeDir, "configure"), fmt.Sprintf("--prefix=%s", prefix))
	cmd.Dir = makeDir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	err = cmd.Start()
	if err != nil {
		return err
	}
	if strings.ToLower(gs.LogstashConfig.Buildpack.LogLevel) == "debug" {
		go gs.copyOutput(stdout, "stdout")
		go gs.copyOutput(stderr, "stderr")
	} else {
		go gs.copyOutput(stdout, "none")
		go gs.copyOutput(stderr, "none")
	}

	err = cmd.Wait()
	if err != nil {
		gs.Log.Info("'Configure' of %s failed", dep.FullName)
		return err
	}

	//make
	gs.Log.Info("Step 2 of 3: make ... (can take up to three minutes!)")
	cmd = exec.Command("make", "-j", "8")
	cmd.Dir = makeDir
	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err = cmd.StderrPipe()
	if err != nil {
		return err
	}

	err = cmd.Start()
	if err != nil {
		return err
	}
	if strings.ToLower(gs.LogstashConfig.Buildpack.LogLevel) == "debug" {
		go gs.copyOutput(stdout, "stdout")
		go gs.copyOutput(stderr, "stderr")
	} else {
		go gs.copyOutput(stdout, "none")
		go gs.copyOutput(stderr, "none")
	}
	err = cmd.Wait()
	if err != nil {
		gs.Log.Info("'Make' of %s failed", dep.FullName)
		return err
	}

	//make install
	gs.Log.Info("Step 3 of 3: make install ...")
	cmd = exec.Command("make", "install")
	cmd.Dir = makeDir
	stdout, err = cmd.StdoutPipe()
	if err != nil {
		return err
	}

	stderr, err = cmd.StderrPipe()
	if err != nil {
		return err
	}

	err = cmd.Start()
	if err != nil {
		return err
	}
	if strings.ToLower(gs.LogstashConfig.Buildpack.LogLevel) == "debug" {
		go gs.copyOutput(stdout, "stdout")
		go gs.copyOutput(stderr, "stderr")
	} else {
		go gs.copyOutput(stdout, "none")
		go gs.copyOutput(stderr, "none")
	}
	cmd.Wait()
	if err != nil {
		gs.Log.Info("'Make Install' of %s failed", dep.FullName)
		return err
	}
	gs.Log.Info("Compilation of %s done", dep.FullName)

	return nil

}

func (gs *Supplier) RemoveUnusedDependencies() error {

	for cachedDep, value := range gs.CachedDeps {
		if value == "" {
			gs.Log.Debug(fmt.Sprintf("--> deleting unused dependency '%s' from application cache", cachedDep))
			os.RemoveAll(filepath.Join(gs.DepCacheDir, cachedDep))
		}
	}
	return nil
}

func (gs *Supplier) SelectDependencyVersion(dependency Dependency) (string, error) {

	dependencyVersion := dependency.ConfigVersion

	if dependencyVersion == "" {
		defaultDependencyVersion, err := gs.Manifest.DefaultVersion(dependency.Name)
		if err != nil {
			return "", err
		}
		dependencyVersion = defaultDependencyVersion.Version
	}

	return gs.parseDependencyVersion(dependency, dependencyVersion)
}

func (gs *Supplier) parseDependencyVersion(dependency Dependency, partialDependencyVersion string) (string, error) {
	existingVersions := gs.Manifest.AllDependencyVersions(dependency.Name)

	if len(strings.Split(partialDependencyVersion, ".")) < dependency.VersionParts {
		partialDependencyVersion += ".x"
	}

	expandedVer, err := libbuildpack.FindMatchingVersion(partialDependencyVersion, existingVersions)
	if err != nil {
		return "", err
	}

	return expandedVer, nil
}

func (gs *Supplier) EvalRuntimeLocation(dependency Dependency) string {
	return filepath.Join(gs.Stager.DepsIdx(), dependency.FullName)
}

func (gs *Supplier) EvalStagingLocation(dependency Dependency) string {
	return filepath.Join(gs.Stager.DepDir(), dependency.FullName)
}

func (gs *Supplier) EvalCacheLocation(dependency Dependency) string {
	return filepath.Join(gs.DepCacheDir, dependency.FullName)
}

func (gs *Supplier) EvalTmpLocation(dependency Dependency) string {
	return filepath.Join(gs.DepTmpDir, dependency.FullName)
}

func (gs *Supplier) EvalTmpExtractLocation(dependency Dependency) string {
	return filepath.Join(gs.DepTmpExtractDir, dependency.FullName)
}

func (gs *Supplier) WriteScript(scriptName, scriptContents string) error {
	scriptsDir := filepath.Join(gs.Stager.DepDir(), "scripts")

	err := os.MkdirAll(scriptsDir, 0755)
	if err != nil {
		return err
	}

	return writeToFile(strings.NewReader(scriptContents), filepath.Join(scriptsDir, scriptName), 0755)
}

func (gs *Supplier) ExecScript(scriptName string) error {
	scriptsDir := filepath.Join(gs.Stager.DepDir(), "scripts")

	cmd := exec.Command("/bin/sh", filepath.Join(scriptsDir, scriptName))

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	err = cmd.Start()
	if err != nil {
		return err
	}

	if strings.ToLower(gs.LogstashConfig.Buildpack.LogLevel) == "debug" {
		go gs.copyOutput(stdout, "stdout")
		go gs.copyOutput(stderr, "stderr")
	} else {
		go gs.copyOutput(stdout, "none")
		go gs.copyOutput(stderr, "none")
	}

	err = cmd.Wait()
	if err != nil {
		return err
	}

	return nil
}

func (gs *Supplier) copyOutput(r io.Reader, output string) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {

		switch output {
		case "stdout":
			gs.Log.Info(scanner.Text())
		case "stderr":
			gs.Log.Error(scanner.Text())
		default:
		}
	}
}

func writeToFile(source io.Reader, destFile string, mode os.FileMode) error {
	err := os.MkdirAll(filepath.Dir(destFile), 0755)
	if err != nil {
		return err
	}

	fh, err := os.OpenFile(destFile, os.O_RDWR|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer fh.Close()

	_, err = io.Copy(fh, source)
	if err != nil {
		return err
	}

	return nil
}

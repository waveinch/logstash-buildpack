package supply

import (
	"github.com/andibrunner/libbuildpack"
	"os"
	"path/filepath"
	"strings"

	"fmt"
	"io/ioutil"
	conf "logstash/config"

	"errors"
	"logstash/util"
	"os/exec"
)

type Manifest interface {
	AllDependencyVersions(string) []string
	DefaultVersion(string) (libbuildpack.Dependency, error)
	InstallDependency(libbuildpack.Dependency, string) error
	InstallDependencyWithCache(libbuildpack.Dependency, string, string) error
	InstallOnlyVersion(string, string) error
	IsCached() bool
	WarnNewerPatch(libbuildpack.Dependency) error
	WarnEndOfLife(libbuildpack.Dependency) error
	GetEntry(libbuildpack.Dependency) (*libbuildpack.ManifestEntry, error)
	FetchDependency(libbuildpack.Dependency, string) error
}

type Stager interface {
	AddBinDependencyLink(string, string) error
	BuildDir() string
	CacheDir() string
	DepDir() string
	DepsIdx() string
	WriteConfigYml(interface{}) error
	WriteEnvFile(string, string) error
	WriteProfileD(string, string) error
}

type Supplier struct {
	Version            string
	Stager             Stager
	Manifest           Manifest
	Log                *libbuildpack.Logger
	BuildpackDir       string
	CachedDeps         map[string]string
	DepCacheDir        string
	DepTmpDir          string
	DepTmpExtractDir   string
	GTE                Dependency
	Jq                 Dependency
	Ofelia             Dependency
	Curator            Dependency
	Python3            Dependency
	OpenJdk            Dependency
	Logstash           Dependency
	LogstashPlugins    Dependency
	XPack              Dependency
	LogstashConfig     conf.LogstashConfig
	TemplatesConfig    conf.TemplatesConfig
	VcapApp            conf.VcapApp
	VcapServices       conf.VcapServices
	ConfigFilesExists  bool
	CuratorFilesExists bool
	TemplatesToInstall []conf.Template
	PluginsToInstall   map[string]string
}

type Dependency struct {
	Name               string
	Version            string
	FullName           string
	VersionParts       int
	ConfigVersion      string
	RuntimeLocation    string
	StagingLocation    string
	CacheLocation      string
	TmpLocation        string
	TmpExtractLocation string
	DoCompile          bool
}

func Run(gs *Supplier) error {

	//Init maps for the Installation
	gs.Version = "v1"
	gs.DepCacheDir = filepath.Join(gs.Stager.CacheDir(), "dependencies", gs.Version)
	gs.DepTmpDir = filepath.Join("/tmp", "dependencies", gs.Version)
	gs.DepTmpExtractDir = filepath.Join("/tmp", "dependencies", gs.Version, "extracted")
	gs.PluginsToInstall = make(map[string]string)
	gs.TemplatesToInstall = []conf.Template{}

	//Eval Logstash file and prepare dir structure
	if err := gs.EvalLogstashFile(); err != nil {
		gs.Log.Error("Unable to evaluate Logstash file: %s", err.Error())
		return err
	}

	//Set log level
	if strings.ToLower(gs.LogstashConfig.Buildpack.LogLevel) == "debug" {
		os.Setenv("BP_DEBUG", "true")
	}

	//Init Cache
	if err := gs.ReadCachedDependencies(); err != nil {
		return err
	}

	//Show Depug Infos
	if err := gs.EvalTestCache(); err != nil {
		gs.Log.Error("Unable to test cache: %s", err.Error())
		return err
	}

	//Prepare dir structure
	if err := gs.PrepareAppDirStructure(); err != nil {
		gs.Log.Error("Unable to prepare directory structure for the app: %s", err.Error())
		return err
	}

	//Eval Templates file
	if err := gs.EvalTemplatesFile(); err != nil {
		gs.Log.Error("Unable to evaluate Templates file: %s", err.Error())
		return err
	}

	//Eval Environment
	if err := gs.EvalEnvironment(); err != nil {
		gs.Log.Error("Unable to evaluate environment: %s", err.Error())
		return err
	}

	//Install Dependencies
	if err := gs.InstallDependencyGTE(); err != nil {
		gs.Log.Error("Error installing dependency GTE: %s", err.Error())
		return err
	}
	if err := gs.InstallDependencyJq(); err != nil {
		gs.Log.Error("Error installing dependency JQ: %s", err.Error())
		return err
	}
	if gs.LogstashConfig.Curator.Install {
		if err := gs.InstallDependencyOfelia(); err != nil {
			gs.Log.Error("Error installing dependency Ofelia: %s", err.Error())
			return err
		}
		if err := gs.InstallDependencyPython3(); err != nil {
			gs.Log.Error("Error installing dependency Python3: %s", err.Error())
			return err
		}
		if err := gs.InstallDependencyCurator(); err != nil {
			gs.Log.Error("Error installing dependency Curator: %s", err.Error())
			return err
		}
		if err := gs.PipInstallCurator(); err != nil {
			gs.Log.Error("Error installing dependency Pip Install Curator: %s", err.Error())
			return err
		}

	}

	if err := gs.InstallDependencyOpenJdk(); err != nil {
		gs.Log.Error("Error installing dependency Open JDK: %s", err.Error())
		return err
	}

	//Prepare Staging Environment
	if err := gs.PrepareStagingEnvironment(); err != nil {
		gs.Log.Error("Error preparing Staging environment: %s", err.Error())
		return err
	}

	//Install templates
	if err := gs.InstallTemplates(); err != nil {
		gs.Log.Error("Unable to install template file: %s", err.Error())
		return err
	}

	//Install User Certificates
	if err := gs.InstallUserCertificates(); err != nil {
		gs.Log.Error("Error installing user certificates: %s", err.Error())
		return err
	}

	//Install Curator/Ofelia
	if err := gs.PrepareCurator(); err != nil {
		gs.Log.Error("Error preparing Curator: %s", err.Error())
		return err
	}

	//Install Logstash
	if err := gs.InstallLogstash(); err != nil {
		gs.Log.Error("Error installing Logstash: %s", err.Error())
		return err
	}

	//Install Logstash Plugins
	if len(gs.PluginsToInstall) > 0 { // there are plugins to install

		//Install Logstash Plugins Dependencies from S3
		for key, _ := range gs.PluginsToInstall {
			if strings.HasPrefix(key, "x-pack") { //is x-pack plugin
				if err := gs.InstallDependencyXPack(); err != nil {
					gs.Log.Error("Error installing dependency X-Pack: %s", err.Error())
					return err
				}
				break
			}
		}

		for key, _ := range gs.PluginsToInstall {
			if !strings.HasPrefix(key, "x-pack") { //other than  x-pack plugin
				if err := gs.InstallDependencyLogstashPlugins(); err != nil {
					gs.Log.Error("Error installing dependency 'Logstash plugins': %s", err.Error())
					return err
				}
				break
			}
		}

		//Install Logstash Plugins
		if err := gs.InstallLogstashPlugins(); err != nil {
			gs.Log.Error("Error installing Logstash plugins: %s", err.Error())
			return err
		}
	}

	//Install Logstash Plugins
	if err := gs.ListLogstashPlugins(); err != nil {
		gs.Log.Error("Error listing Logstash plugins: %s", err.Error())
		return err
	}

	//check Logstash config
	if gs.LogstashConfig.ConfigCheck {
		if err := gs.CheckLogstash(); err != nil {
			gs.Log.Error("Error checking configuration : %s", err.Error())
			return err
		}

	}

	// Remove orphand dependencies from application cache
	gs.RemoveUnusedDependencies()

	//WriteConfigYml
	config := map[string]string{
		"LogstashVersion": gs.Logstash.Version,
	}

	if err := gs.Stager.WriteConfigYml(config); err != nil {
		gs.Log.Error("Error writing config.yml: %s", err.Error())
		return err
	}

	return nil
}

func (gs *Supplier) EvalTestCache() error {

	if strings.ToLower(gs.LogstashConfig.Buildpack.LogLevel) == "debug" {
		gs.Log.Debug("----> Show staging directories:")
		gs.Log.Debug("        Cache dir: %s", gs.Stager.CacheDir())
		gs.Log.Debug("        Build dir: %s", gs.Stager.BuildDir())
		gs.Log.Debug("        Buildpack dir: %s", gs.BPDir())
		gs.Log.Debug("        Dependency dir: %s", gs.Stager.DepDir())
		gs.Log.Debug("        DepsIdx: %s", gs.Stager.DepsIdx())

	}
	return nil
}

func (gs *Supplier) EvalLogstashFile() error {
	const configCheck = false
	const reservedMemory = 300
	const heapPersentage = 90
	const logLevel = "Info"
	const noCache = false
	const curatorInstall = false

	gs.LogstashConfig = conf.LogstashConfig{
		Set:            true,
		ConfigCheck:    configCheck,
		ReservedMemory: reservedMemory,
		HeapPercentage: heapPersentage,
		Curator:        conf.Curator{Set: true, Install: curatorInstall},
		Buildpack:      conf.Buildpack{Set: true, LogLevel: logLevel, NoCache: noCache}}

	logstashFile := filepath.Join(gs.Stager.BuildDir(), "Logstash")

	data, err := ioutil.ReadFile(logstashFile)
	if err != nil {
		return err
	}
	if err := gs.LogstashConfig.Parse(data); err != nil {
		return err
	}

	if !gs.LogstashConfig.Set {
		gs.LogstashConfig.HeapPercentage = heapPersentage
		gs.LogstashConfig.ReservedMemory = reservedMemory
		gs.LogstashConfig.ConfigCheck = configCheck
	}
	if !gs.LogstashConfig.Curator.Set {
		gs.LogstashConfig.Curator.Install = curatorInstall //not really needed but maybe we will switch to true later
	}
	if !gs.LogstashConfig.Buildpack.Set {
		gs.LogstashConfig.Buildpack.LogLevel = logLevel
		gs.LogstashConfig.Buildpack.NoCache = noCache
	}

	/*	//Eval X-Pack
		if gs.LogstashConfig.XPack.Monitoring.Enabled || gs.LogstashConfig.XPack.Management.Enabled{
			gs.LogstashConfig.Plugins = append(gs.LogstashConfig.Plugins, "x-pack")

			if gs.LogstashConfig.XPack.Management.Interval == ""{
				gs.LogstashConfig.XPack.Management.Interval = "10s"
			}
			if gs.LogstashConfig.XPack.Monitoring.Interval == ""{
				gs.LogstashConfig.XPack.Monitoring.Interval = "10s"
			}

		}
	*/
	//ToDo Eval values
	if gs.LogstashConfig.Curator.Schedule == "" {
		gs.LogstashConfig.Curator.Schedule = "@daily"
	}

	//copy the user defined plugins to the PluginsToInstall map
	for i := 0; i < len(gs.LogstashConfig.Plugins); i++ {
		gs.PluginsToInstall[gs.LogstashConfig.Plugins[i]] = ""
	}

	return nil
}

func (gs *Supplier) PrepareAppDirStructure() error {

	//create dir conf.d in DepDir
	dir := filepath.Join(gs.Stager.DepDir(), "conf.d")
	err := os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	//create dir logstash.conf.d in DepDir
	dir = filepath.Join(gs.Stager.DepDir(), "logstash.conf.d")
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	//create dir grok-patterns in DepDir
	dir = filepath.Join(gs.Stager.DepDir(), "grok-patterns")
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	//create dir plugins in DepDir
	dir = filepath.Join(gs.Stager.DepDir(), "plugins")
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	//create dir curator.d in DepDir
	dir = filepath.Join(gs.Stager.DepDir(), "curator.d")
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	//create dir scripts in DepDir
	dir = filepath.Join(gs.Stager.DepDir(), "scripts")
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	//create dir curator in DepDir
	dir = filepath.Join(gs.Stager.DepDir(), "curator")
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	//create dir ofelia/scripts in DepDir
	dir = filepath.Join(gs.Stager.DepDir(), "ofelia", "scripts")
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	//create dir ofelia/config in DepDir
	dir = filepath.Join(gs.Stager.DepDir(), "ofelia", "config")
	err = os.MkdirAll(dir, 0755)
	if err != nil {
		return err
	}

	return nil
}

func (gs *Supplier) EvalTemplatesFile() error {

	const credHostField = "host"
	const credUsernameField = "username"
	const credPasswordField = "password"

	gs.TemplatesConfig = conf.TemplatesConfig{
		Set:   true,
		Alias: conf.Alias{Set: true, CredentialsHostField: credHostField, CredentialsUsernameField: credUsernameField, CredentialsPasswordField: credPasswordField},
	}
	templateFile := filepath.Join(gs.BPDir(), "defaults/templates/templates.yml")

	data, err := ioutil.ReadFile(templateFile)
	if err != nil {
		return err
	}
	if err := gs.TemplatesConfig.Parse(data); err != nil {
		return err
	}
	if !gs.TemplatesConfig.Alias.Set {
		gs.TemplatesConfig.Alias.CredentialsHostField = credHostField
		gs.TemplatesConfig.Alias.CredentialsUsernameField = credUsernameField
		gs.TemplatesConfig.Alias.CredentialsPasswordField = credPasswordField
	}

	return nil
}

func (gs *Supplier) EvalEnvironment() error {

	//get VCAP_APPLICATIOM
	gs.VcapApp = conf.VcapApp{}
	dataApp := os.Getenv("VCAP_APPLICATION")
	if err := gs.VcapApp.Parse([]byte(dataApp)); err != nil {
		return err
	}

	// get VCAP_SERVICES
	gs.VcapServices = conf.VcapServices{}
	dataServices := os.Getenv("VCAP_SERVICES")
	if err := gs.VcapServices.Parse([]byte(dataServices)); err != nil {
		return err
	}

	//check if files (also directories) exist in the application's "conf.d" directory
	configDir := filepath.Join(gs.Stager.BuildDir(), "conf.d")
	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		gs.ConfigFilesExists = false
		return nil
	}

	files, err := ioutil.ReadDir(configDir)
	if err != nil {
		return err
	}
	if len(files) > 0 {
		gs.ConfigFilesExists = true
	}

	//check if curator files (also directories) exist in the application's "curator.d" directory
	curatorDir := filepath.Join(gs.Stager.BuildDir(), "curator.d")
	if _, err := os.Stat(curatorDir); os.IsNotExist(err) {
		gs.CuratorFilesExists = false
		return nil
	}

	curatorFiles, err := ioutil.ReadDir(curatorDir)
	if err != nil {
		return err
	}
	if len(curatorFiles) > 0 {
		gs.CuratorFilesExists = true
	}

	return nil
}

func (gs *Supplier) InstallDependencyGTE() error {
	var err error

	gs.GTE, err = gs.NewDependency("gte", 3, "", false)
	if err != nil {
		return err
	}

	if err := gs.InstallDependency(gs.GTE); err != nil {
		return err
	}

	content := util.TrimLines(fmt.Sprintf(`
				export GTE_HOME=$DEPS_DIR/%s
				PATH=$PATH:$GTE_HOME
				`, gs.GTE.RuntimeLocation))

	if err := gs.WriteDependencyProfileD(gs.GTE.Name, content); err != nil {
		return err
	}

	return nil
}

func (gs *Supplier) InstallDependencyJq() error {
	var err error

	gs.Jq, err = gs.NewDependency("jq", 3, "", false)
	if err != nil {
		return err
	}

	if err := gs.InstallDependency(gs.Jq); err != nil {
		return err
	}

	content := util.TrimLines(fmt.Sprintf(`
				export JQ_HOME=$DEPS_DIR/%s
				PATH=$PATH:$JQ_HOME
				`, gs.Jq.RuntimeLocation))

	if err := gs.WriteDependencyProfileD(gs.Jq.Name, content); err != nil {
		return err
	}
	return nil
}

func (gs *Supplier) InstallDependencyOfelia() error {
	var err error
	gs.Ofelia, err = gs.NewDependency("ofelia", 3, "", false)
	if err != nil {
		return err
	}

	if err := gs.InstallDependency(gs.Ofelia); err != nil {
		return err
	}

	content := util.TrimLines(fmt.Sprintf(`
				export OFELIA_HOME=$DEPS_DIR/%s
				PATH=$PATH:$OFELIA_HOME
				`, gs.Ofelia.RuntimeLocation))

	if err := gs.WriteDependencyProfileD(gs.Ofelia.Name, content); err != nil {
		return err
	}
	return nil
}

func (gs *Supplier) InstallDependencyPython3() error {

	var err error
	gs.Python3, err = gs.NewDependency("python3", 3, "", true)
	if err != nil {
		return err
	}

	if err := gs.InstallDependency(gs.Python3); err != nil {
		return err
	}

	content := util.TrimLines(fmt.Sprintf(`
				export PYTHONHOME=$DEPS_DIR/%s
				PATH=${PYTHONHOME}/bin:${PATH}
				`, gs.Python3.RuntimeLocation))

	if err := gs.WriteDependencyProfileD(gs.Python3.Name, content); err != nil {
		return err
	}

	return nil
}

func (gs *Supplier) InstallDependencyCurator() error {

	var err error
	gs.Curator, err = gs.NewDependency("curator", 3, "", false)
	if err != nil {
		return err
	}

	if err := gs.InstallDependency(gs.Curator); err != nil {
		return err
	}

	content := util.TrimLines(fmt.Sprintf(`
				export CURATOR_HOME=$DEPS_DIR/%s/%s
				export PYTHONPATH=${CURATOR_HOME}/lib/python3.6/site-packages
				PATH=${CURATOR_HOME}/bin:${PATH}
				`, gs.Stager.DepsIdx(), "curator"))

	if err := gs.WriteDependencyProfileD(gs.Curator.Name, content); err != nil {
		return err
	}
	return nil
}

func (gs *Supplier) PipInstallCurator() error {

	scriptName := "pip_install_curator"
	content := util.TrimLines(fmt.Sprintf(`
				#!/bin/bash
				export PATH=%s/bin:$PATH
    			# --no-index prevents contacting pypi to download packages
    			# --find-links tells pip where to look for the dependancies
    			pip3 install --no-index --find-links %s/dependencies --install-option="--prefix=%s/curator" elasticsearch-curator -v
				pip3 list
				`, filepath.Join(gs.Python3.StagingLocation), filepath.Join(gs.Curator.StagingLocation), filepath.Join(gs.Stager.DepDir())))

	if err := gs.WriteScript(scriptName, content); err != nil {
		gs.Log.Error("Error WriteScript %s: %s", scriptName, err.Error())
		return err
	}

	if err := gs.ExecScript(scriptName); err != nil {
		gs.Log.Error("Error ExecScript %s: %s", scriptName, err.Error())
		return err
	}

	return nil
}

func (gs *Supplier) PrepareCurator() error {

	//create Curator start script
	content := util.TrimLines(fmt.Sprintf(`
				#!/bin/bash
				export LC_ALL=en_US.UTF-8
				export LANG=en_US.UTF-8
				${PYTHONHOME}/bin/python3 ${CURATOR_HOME}/bin/curator --config ${HOME}/curator.conf.d/curator.yml ${HOME}/curator.conf.d/actions.yml
				`))

	err := ioutil.WriteFile(filepath.Join(gs.Stager.DepDir(), "ofelia", "scripts", "curator.sh"), []byte(content), 0755)
	if err != nil {
		gs.Log.Error("Unable to create Curator start script: %s", err.Error())
		return err
	}

	//create ofelia schedule.ini
	content = util.TrimLines(fmt.Sprintf(`
				[job-local "curator"]
				schedule = %s
				command = {{- .Env.HOME -}}/bin/curator.sh
				`,
		gs.LogstashConfig.Curator.Schedule))

	err = ioutil.WriteFile(filepath.Join(gs.Stager.DepDir(), "ofelia", "config", "schedule.ini"), []byte(content), 0644)
	if err != nil {
		gs.Log.Error("Unable to create Ofelia schedule.ini: %s", err.Error())
		return err
	}

	// pre-processing of curator config templates
	templateFile := filepath.Join(gs.BPDir(), "defaults/curator")
	destFile := filepath.Join(gs.Stager.DepDir(), "curator.d")

	err = exec.Command(fmt.Sprintf("%s/gte", gs.GTE.StagingLocation), "-d", "<<:>>", templateFile, destFile).Run()
	if err != nil {
		gs.Log.Error("Error pre-processing curator config templates: %s", err.Error())
		return err
	}

	return nil
}

func (gs *Supplier) InstallDependencyOpenJdk() error {
	var err error
	gs.OpenJdk, err = gs.NewDependency("openjdk", 3, "", false)
	if err != nil {
		return err
	}

	if err := gs.InstallDependency(gs.OpenJdk); err != nil {
		return err
	}

	content := util.TrimLines(fmt.Sprintf(`
				export JAVA_HOME=$DEPS_DIR/%s
				PATH=$PATH:$JAVA_HOME/bin
				`, gs.OpenJdk.RuntimeLocation))

	if err := gs.WriteDependencyProfileD(gs.OpenJdk.Name, content); err != nil {
		return err
	}
	return nil
}

func (gs *Supplier) InstallDependencyXPack() error {

	//Install x-pack from S3
	var err error
	gs.XPack, err = gs.NewDependency("x-pack", 3, gs.LogstashConfig.Version, false) //same version as Logstash
	if err != nil {
		return err
	}

	if err := gs.InstallDependency(gs.XPack); err != nil {
		return err
	}

	return nil
}

func (gs *Supplier) InstallDependencyLogstashPlugins() error {

	//Install logstash-plugins from S3
	var err error
	gs.LogstashPlugins, err = gs.NewDependency("logstash-plugins", 3, gs.LogstashConfig.Version, false) //same version as Logstash
	if err != nil {
		return err
	}

	if err := gs.InstallDependency(gs.LogstashPlugins); err != nil {
		return err
	}

	return nil
}

func (gs *Supplier) InstallLogstash() error {
	var err error
	gs.Logstash, err = gs.NewDependency("logstash", 3, gs.LogstashConfig.Version, false)
	if err != nil {
		return err
	}

	if err := gs.InstallDependency(gs.Logstash); err != nil {
		return err
	}

	curatorEnabled := ""
	if gs.LogstashConfig.Curator.Install {
		curatorEnabled = "enabled"
	}

	sleepCommand := ""
	if gs.LogstashConfig.Buildpack.DoSleepCommand {
		sleepCommand = "yes"
	}

	content := util.TrimLines(fmt.Sprintf(`
			export LS_BP_RESERVED_MEMORY=%d
			export LS_BP_HEAP_PERCENTAGE=%d
			export LS_BP_JAVA_OPTS=%s
			export LS_CMD_ARGS=%s
			export LS_ROOT=$DEPS_DIR/%s
			export LS_CURATOR_ENABLED=%s
			export LS_DO_SLEEP=%s
			export LOGSTASH_HOME=$DEPS_DIR/%s
			PATH=$PATH:$LOGSTASH_HOME/bin
			`,
		gs.LogstashConfig.ReservedMemory,
		gs.LogstashConfig.HeapPercentage,
		gs.LogstashConfig.JavaOpts,
		gs.LogstashConfig.CmdArgs,
		gs.Stager.DepsIdx(),
		curatorEnabled,
		sleepCommand,
		gs.Logstash.RuntimeLocation))

	if err := gs.WriteDependencyProfileD(gs.Logstash.Name, content); err != nil {
		gs.Log.Error("Error writing profile.d script for Logstash: %s", err.Error())
		return err
	}
	return nil
}

func (gs *Supplier) PrepareStagingEnvironment() error {
	vmOptions := gs.LogstashConfig.JavaOpts

	if vmOptions != "" {
		os.Setenv("LS_JAVA_OPTS", fmt.Sprintf("%s", vmOptions))
	} else {
		mem := (gs.VcapApp.Limits.Mem - gs.LogstashConfig.ReservedMemory) / 100 * gs.LogstashConfig.HeapPercentage
		os.Setenv("LS_JAVA_OPTS", fmt.Sprintf("-Xmx%dm -Xms%dm", mem, mem))
	}

	os.Setenv("JAVA_HOME", gs.OpenJdk.StagingLocation)
	os.Setenv("PATH", os.Getenv("PATH")+":"+gs.OpenJdk.StagingLocation+"/bin")
	os.Setenv("PORT", "8080") //dummy PORT: used by template processing for logstash check

	if strings.ToLower(gs.LogstashConfig.Buildpack.LogLevel) == "debug" {
		gs.Log.Debug(" ### JAVA_HOME %s", os.Getenv("JAVA_HOME"))
		gs.Log.Debug(" ### PATH %s", os.Getenv("PATH"))
		gs.Log.Debug(" ### LS_JAVA_OPTS %s", os.Getenv("LS_JAVA_OPTS"))
	}
	return nil
}

func (gs *Supplier) InstallUserCertificates() error {

	if len(gs.LogstashConfig.Certificates) == 0 { // no certificates to install
		return nil
	}

	localCerts, _ := gs.ReadLocalCertificates(gs.Stager.BuildDir() + "/certificates")

	for i := 0; i < len(gs.LogstashConfig.Certificates); i++ {

		localCert := localCerts[gs.LogstashConfig.Certificates[i]]

		if localCert != "" {
			gs.Log.Info(fmt.Sprintf("----> installing user certificate '%s' to TrustStore ... ", gs.LogstashConfig.Certificates[i]))
			certToInstall := gs.Stager.BuildDir() + "/certificates/" + localCert
			out, err := exec.Command(fmt.Sprintf("%s/bin/keytool", gs.OpenJdk.StagingLocation), "-import", "-trustcacerts", "-keystore", fmt.Sprintf("%s/jre/lib/security/cacerts", gs.OpenJdk.StagingLocation), "-storepass", "changeit", "-noprompt", "-alias", gs.LogstashConfig.Certificates[i], "-file", certToInstall).CombinedOutput()
			gs.Log.Info(string(out))
			if err != nil {
				gs.Log.Warning("Error installing user certificate '%s' to TrustStore: %s", gs.LogstashConfig.Certificates[i], err.Error())
			}
		} else {
			err := errors.New("crt file for certificate not found in directory")
			gs.Log.Error("File %s.crt not found in directory '/certificates'", gs.LogstashConfig.Certificates[i])
			return err
		}
	}

	return nil

}

func (gs *Supplier) InstallTemplates() error {

	if !gs.ConfigFilesExists && len(gs.LogstashConfig.ConfigTemplates) == 0 {
		// install all default templates

		//copy default templates to config
		for _, t := range gs.TemplatesConfig.Templates {

			if t.IsDefault {

				if len(t.Tags) > 0 {
					vcapServices := []conf.VcapService{}
					vcapServicesWithTag := gs.VcapServices.WithTags(t.Tags)
					vcapServicesUserProvided := gs.VcapServices.UserProvided()

					if len(vcapServicesWithTag) > 0 {
						vcapServices = append(vcapServices, vcapServicesWithTag...)
					}
					if len(vcapServicesUserProvided) > 0 {
						vcapServices = append(vcapServices, vcapServicesUserProvided...)
					}

					if len(vcapServices) == 0 {

						if gs.LogstashConfig.EnableServiceFallback {
							ti := t
							ti.ServiceInstanceName = ""
							gs.TemplatesToInstall = append(gs.TemplatesToInstall, ti)
							gs.Log.Warning("No service found for template %s, will do the fallback. Please bind a service and restage the app", ti.Name)
						} else {
							return errors.New("no service found for template")
						}
					} else if len(vcapServices) > 1 {
						return errors.New("more than one service found for template")
					} else {
						ti := t
						ti.ServiceInstanceName = vcapServices[0].Name
						gs.TemplatesToInstall = append(gs.TemplatesToInstall, ti)
					}
				} else {
					ti := t
					ti.ServiceInstanceName = ""
					gs.TemplatesToInstall = append(gs.TemplatesToInstall, ti)
				}
			}
		}

	} else {
		//only install explicitly defined templates, if any
		//check them all

		for _, ct := range gs.LogstashConfig.ConfigTemplates {
			found := false
			templateName := strings.Trim(ct.Name, " ")
			if len(templateName) == 0 {
				gs.Log.Warning("Skipping template: no valid name defined for template in Logstash file")
				continue
			}
			for _, t := range gs.TemplatesConfig.Templates {
				if templateName == t.Name {
					serviceInstanceName := strings.Trim(ct.ServiceInstanceName, " ")
					if len(serviceInstanceName) == 0 && len(t.Tags) > 0 {
						gs.Log.Error("No service instance name defined for template %s in Logstash file", templateName)
						return errors.New("no service instance name defined for template in Logstash file")
					}

					ti := t
					if len(serviceInstanceName) > 0 && len(t.Tags) == 0 {
						gs.Log.Warning("Service instance name '%s' is defined for template %s in Logstash file but template can not be bound to a service.", serviceInstanceName, templateName)
					} else {
						ti.ServiceInstanceName = serviceInstanceName
					}
					gs.TemplatesToInstall = append(gs.TemplatesToInstall, ti)

					found = true
					break
				}
			}
			if !found {
				gs.Log.Warning("Template %s defined in Logstash file does not exist", templateName)
			}
		}
	}

	os.Setenv("SERVICE_INSTANCE_NAME", vcapServices[0].Name)
	os.Setenv("CREDENTIALS_HOST_FIELD", gs.TemplatesConfig.Alias.CredentialsHostField)
	os.Setenv("CREDENTIALS_USERNAME_FIELD", gs.TemplatesConfig.Alias.CredentialsUsernameField)
	os.Setenv("CREDENTIALS_PASSWORD_FIELD", gs.TemplatesConfig.Alias.CredentialsPasswordField)

	//copy templates --> conf.d
	for _, ti := range gs.TemplatesToInstall {

		if len(gs.LogstashConfig.LogstashCredentials.Username) > 0 {
			os.Setenv("LOGSTASH_AUTH", "true")
		} else {
			os.Setenv("LOGSTASH_AUTH", "false")
		}
		os.Setenv("LOGSTASH_USERNAME", gs.LogstashConfig.LogstashCredentials.Username)
		os.Setenv("LOGSTASH_PASSWORD", gs.LogstashConfig.LogstashCredentials.Password)

		templateFile := filepath.Join(gs.BPDir(), "defaults/templates/", ti.Name+".conf")
		destFile := filepath.Join(gs.Stager.DepDir(), "conf.d", ti.Name+".conf")

		err := exec.Command(fmt.Sprintf("%s/gte", gs.GTE.StagingLocation), "-d", "<<:>>", templateFile, destFile).Run()
		if err != nil {
			gs.Log.Error("Error pre-processing template %s: %s", ti.Name, err.Error())
			return err
		}

	}

	// copy grok-patterns and plugins
	var groksToInstall map[string]string

	groksToInstall = make(map[string]string)

	for i := 0; i < len(gs.TemplatesToInstall); i++ {

		for g := 0; g < len(gs.TemplatesToInstall[i].Groks); g++ {
			groksToInstall[gs.TemplatesToInstall[i].Groks[g]] = ""
		}
		for p := 0; p < len(gs.TemplatesToInstall[i].Plugins); p++ {
			gs.PluginsToInstall[gs.TemplatesToInstall[i].Plugins[p]] = ""
		}
	}

	for key, _ := range groksToInstall {
		grokFile := filepath.Join(gs.BPDir(), "defaults/grok-patterns", key)
		destFile := filepath.Join(gs.Stager.DepDir(), "grok-patterns", key)

		err := exec.Command(fmt.Sprintf("%s/gte", gs.GTE.StagingLocation), "-d", "<<:>>", grokFile, destFile).Run()
		if err != nil {
			gs.Log.Error("Error pre-processing grok-patterns template %s: %s", key, err.Error())
			return err
		}
	}

	//default Plugins will be installed in method "InstallLogstashPlugins"

	return nil
}

func (gs *Supplier) ListLogstashPlugins() error {
	gs.Log.Info("----> Listing all installed Logstash plugins ...")

	out, err := exec.Command(fmt.Sprintf("%s/bin/logstash-plugin", gs.Logstash.StagingLocation), "list", "--verbose").CombinedOutput()
	gs.Log.Info(string(out))
	if err != nil {
		gs.Log.Error("Error listing all installed Logstash plugins: %s", err.Error())
		return err
	}
	return nil
}

func (gs *Supplier) InstallLogstashPlugins() error {

	xPackPlugins, _ := gs.ReadLocalPlugins(gs.XPack.StagingLocation)
	defaultPlugins, _ := gs.ReadLocalPlugins(gs.LogstashPlugins.StagingLocation)
	userPlugins, _ := gs.ReadLocalPlugins(gs.Stager.BuildDir() + "/plugins")

	gs.Log.Info("----> Installing Logstash plugins ...")
	for key, _ := range gs.PluginsToInstall {
		//Priorisation
		xpackPlugin := gs.GetLocalPlugin(key, xPackPlugins)
		defaultPlugin := gs.GetLocalPlugin(key, defaultPlugins)
		userPlugin := gs.GetLocalPlugin(key, userPlugins)

		pluginToInstall := ""

		if xpackPlugin != "" {
			pluginToInstall = filepath.Join(gs.XPack.StagingLocation, xpackPlugin) // Prio 1 (offline installation)
		} else if defaultPlugin != "" {
			pluginToInstall = filepath.Join(gs.LogstashPlugins.StagingLocation, defaultPlugin) // Prio 2 (offline installation)
		} else if userPlugin != "" {
			pluginToInstall = filepath.Join(gs.Stager.BuildDir(), "plugins", userPlugin) // Prio 3 (offline installation)
		} else {
			pluginToInstall = key // Prio 4 (online installation)
		}

		if strings.HasSuffix(pluginToInstall, ".zip") {
			pluginToInstall = "file://" + pluginToInstall
		}

		//Install Plugin
		out, err := exec.Command(fmt.Sprintf("%s/bin/logstash-plugin", gs.Logstash.StagingLocation), "install", pluginToInstall).CombinedOutput()
		if err != nil {
			gs.Log.Error(string(out))
			gs.Log.Error("Error installing Logstash plugin %s: %s", key, err.Error())
			return err
		}
	}

	return nil
}

func (gs *Supplier) CheckLogstash() error {

	gs.Log.Info("----> Starting Logstash config check...")

	// template processing for check
	templateDir := filepath.Join(gs.Stager.DepDir(), "conf.d")
	destDir := filepath.Join(gs.Stager.DepDir(), "logstash.conf.d")
	err := exec.Command(fmt.Sprintf("%s/gte", gs.GTE.StagingLocation), templateDir, destDir).Run()
	if err != nil {
		gs.Log.Error("Error processing templates for Logstash config check: %s", err.Error())
		return err
	}

	// list files in logstash.conf.d
	file, err := os.Open(destDir)
	if err != nil {
		gs.Log.Error("  --> failed opening logstash.conf.d directory: %s", err)
		return err
	}
	defer file.Close()

	gs.Log.Info("  --> Listing files in logstash.conf.d directory ...")
	list, _ := file.Readdirnames(0) // 0 to read all files
	found := false
	for _, name := range list {
		found = true
		gs.Log.Info("      " + name)
	}
	if !found {
		gs.Log.Warning("      " + "no files found")
	}

	gs.Log.Info("  --> Checking Logstash config ...")
	// check logstash config
	out, err := exec.Command(fmt.Sprintf("%s/bin/logstash", gs.Logstash.StagingLocation), "-f", destDir, "-t").CombinedOutput()
	gs.Log.Info(string(out))
	if err != nil {
		gs.Log.Error("Error checking Logstash config: %s", err.Error())
		return err
	}

	gs.Log.Info("  --> Finished Logstash config check...")

	return nil
}

func (gs *Supplier) ReadLocalCertificates(filePath string) (map[string]string, error) {

	var localCerts map[string]string
	localCerts = make(map[string]string)

	file, err := os.Open(filePath)
	if err != nil {
		gs.Log.Error("failed opening certificates directory: %s", err)
		return localCerts, err
	}
	defer file.Close()

	list, _ := file.Readdirnames(0) // 0 to read all files and folders
	for _, name := range list {

		if strings.HasSuffix(name, ".crt") {
			certParts := strings.Split(name, ".crt")

			if len(certParts) == 2 {
				certName := certParts[0]
				localCerts[certName] = name
			}

		}
	}

	return localCerts, nil
}

func (gs *Supplier) ReadLocalPlugins(filePath string) ([]string, error) {

	file, err := os.Open(filePath)
	if err != nil {
		return []string{}, nil
	}
	defer file.Close()

	list, _ := file.Readdirnames(0) // 0 to read all files and folders

	return list, nil
}

func (gs *Supplier) GetLocalPlugin(pluginName string, pluginFileNames []string) string {

	for i := 0; i < len(pluginFileNames); i++ {
		if strings.HasPrefix(pluginFileNames[i], pluginName) {
			return pluginFileNames[i]
		}
	}

	return ""
}

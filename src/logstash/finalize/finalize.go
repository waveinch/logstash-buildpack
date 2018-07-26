package finalize

import (
	"fmt"
	"github.com/andibrunner/libbuildpack"
	"golang"
	"io"
	"io/ioutil"
	"logstash/util"
	"os"
	"path/filepath"
)

type Command interface {
	Execute(string, io.Writer, io.Writer, string, ...string) error
}

type Stager interface {
	BuildDir() string
	ClearDepDir() error
	DepDir() string
	DepsIdx() string
	WriteProfileD(string, string) error
}

type Finalizer struct {
	Stager  Stager
	Command Command
	Log     *libbuildpack.Logger
}

func NewFinalizer(stager Stager, command Command, logger *libbuildpack.Logger) (*Finalizer, error) {
	config := struct {
		Config struct {
			LogstashVersion string `yaml:"LogstashVersion"`
		} `yaml:"config"`
	}{}
	if err := libbuildpack.NewYAML().Load(filepath.Join(stager.DepDir(), "config.yml"), &config); err != nil {
		logger.Error("Unable to read config.yml: %s", err.Error())
		return nil, err
	}

	return &Finalizer{
		Stager:  stager,
		Command: command,
		Log:     logger,
	}, nil
}

func Run(gf *Finalizer) error {

	if err := os.MkdirAll(filepath.Join(gf.Stager.BuildDir(), "bin"), 0755); err != nil {
		gf.Log.Error("Unable to create <build-dir>/bin: %s", err.Error())
		return err
	}

	if err := gf.CreateStartupEnvironment("/tmp"); err != nil {
		gf.Log.Error("Unable to create startup scripts: %s", err.Error())
		return err
	}

	return nil
}

func (gf *Finalizer) CreateStartupEnvironment(tempDir string) error {

	//create start script

	content := util.TrimLines(fmt.Sprintf(`
				echo "--> STARTING UP ..."
				MemLimits="$(echo ${VCAP_APPLICATION} | $JQ_HOME/jq '.limits.mem')"

				echo "--> container memory limit = ${MemLimits}m"
				if [ -n "$LS_BP_JAVA_OPTS" ] || [ -z "$MemLimits" ] || [ -z "$LS_BP_RESERVED_MEMORY"  ] || [ -z "$LS_BP_HEAP_PERCENTAGE" ] ; then
					export LS_JAVA_OPTS=$LS_BP_JAVA_OPTS
					echo "--> Using JAVA_OPTS=\"${LS_JAVA_OPTS}\" (user defined)"
				else
					HeapSize=$(( ($MemLimits - $LS_BP_RESERVED_MEMORY) / 100 * $LS_BP_HEAP_PERCENTAGE ))
					export LS_JAVA_OPTS="-Xmx${HeapSize}m -Xms${HeapSize}m"
					echo "--> Using JAVA_OPTS=\"${LS_JAVA_OPTS}\" (calculated)"
				fi

				echo "--> preparing runtime directories ..."
				mkdir -p conf.d
				mkdir -p grok-patterns
				mkdir -p curator.d

				if [ -d logstash.conf.d ] ; then
					rm -rf logstash.conf.d
				fi
				mkdir -p logstash.conf.d

				if [ -d curator.conf.d ] ; then
					rm -rf curator.conf.d
				fi
				mkdir -p curator.conf.d

				if [ -d ofelia ] ; then
					rm -rf ofelia
				fi
				mkdir -p ofelia

				echo "--> template processing ..."
				$GTE_HOME/gte $HOME/conf.d $HOME/logstash.conf.d
				$GTE_HOME/gte $LS_ROOT/conf.d $HOME/logstash.conf.d

				#no template processing for grok-patterns

				$GTE_HOME/gte $LS_ROOT/grok-patterns $HOME/grok-patterns

				$GTE_HOME/gte $HOME/curator.d $HOME/curator.conf.d
				$GTE_HOME/gte -n $LS_ROOT/curator.d $HOME/curator.conf.d

				# copy logstash.yml
				if [ -f $HOME/logstash.yml ] ; then
					$GTE_HOME/gte $HOME/logstash.yml $LOGSTASH_HOME/config/logstash.yml
				fi

				$GTE_HOME/gte $LS_ROOT/ofelia/scripts $HOME/bin
				$GTE_HOME/gte $LS_ROOT/ofelia/config $HOME/ofelia

				echo "--> STARTING LOGSTASH ..."
				if [ -n "$LS_CMD_ARGS" ] ; then
					echo "--> using LS_CMD_ARGS=\"$LS_CMD_ARGS\""
				fi

				if [ -n "$LS_DO_SLEEP" ] ; then
					sleep 3600
				fi

				chmod +x $HOME/bin/*.sh

				if [ -n "$LS_CURATOR_ENABLED" ] ; then
					echo "--> running Curator once to create the Logstash index for today"
					${HOME}/bin/curator.sh

					echo "--> starting Ofelia for Curator in the background"
					$OFELIA_HOME/ofelia daemon --config ${HOME}/ofelia/schedule.ini 2>&1 &
				fi
				if [ -f $HOME/logstash.yml ] ; then
					grep -q pipeline logstash.yml
					XPACK-PIPELINES=$?
				fi

				if [ $XPACK-PIPELINES -eq 0 ] ; then
					$LOGSTASH_HOME/bin/logstash $LS_CMD_ARGS
				elif
					$LOGSTASH_HOME/bin/logstash -f logstash.conf.d $LS_CMD_ARGS
				fi
				`))

	err := ioutil.WriteFile(filepath.Join(gf.Stager.BuildDir(), "bin/run.sh"), []byte(content), 0755)
	if err != nil {
		gf.Log.Error("Unable to write start script: %s", err.Error())
		return err
	}

	//create release yml
	err = ioutil.WriteFile(filepath.Join(tempDir, "buildpack-release-step.yml"), []byte(golang.ReleaseYAML("bin/run.sh")), 0644)
	if err != nil {
		gf.Log.Error("Unable to write release yml: %s", err.Error())
		return err
	}

	return gf.Stager.WriteProfileD("go.sh", golang.GoScript())
}

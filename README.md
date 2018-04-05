# Logstash Buildpack for Cloud Foundry

This buildpack allows to deploy [Logstash](https://www.elastic.co/products/logstash) as an app in Cloud Foundry.
The buildpack also includes curator, which allows to manage the indices in Elasticsearch.




## Use Cases

<img src="images/use_cases.png" alt="Use CAses" width="600">


### Use Case "automatic"


> You want to automatically connect to an Elasticsearch service and listen to Syslog messages.

In this case you have nothing to configure. Just deploy an empty `Logstash` file and use a Cloud Foundry `manifest.yml` file where you bind a service instance with your app. 

The buildpack is only able to do a connection if exactly one service of the same service type (Elasticsearch) is bound to the app. The buildpack finds the service by comparing the service tags (they should be set as "elasticsearch' or "elastic"). You can set `enable-service-fallback`to `true`: in this case `stdout` instead of `elasticsearch` will be applied as output when no service is found. 


### Use Case "manual":

> You don't want to use pre-defined templates and you deliver all Logtsash config files in the expected file structure as described later. You are still able to deliver additional plugins and certificates but you are responsible for the service bindings.

When you deliver config files in the "conf.d" folder no pre-defined templates are applied by default. 

#### Example `manual Logstash` file:

```
plugins:
- logstash-input-kafka
- logstash-output-kafka
certificates:
- elasticsearch
curator:
  install: false
```



### Use Case "mixed":


> You want to use only some of pre-defined templates and you deliver also some own Logtsash config files in the expected file structure. Depending of which templates you use you are responsible for the service bindings or not. You are able to deliver additional plugins and certificates.

When you deliver config files in the "conf.d" folder no pre-defined templates are applied by default. If you still want to use some pre-defined templates you have to explicitly define them in the "Logstash" file.

#### Example `mixed Logstash` file with automatic binding:

```
config-templates:
- name: cf-output-elasticsearch
  service-instance-name: my-elasticsearch
plugins:
- logstash-input-kafka
- logstash-output-kafka
certificates:
- elasticsearch
curator:
  install: false
```


## Usage

### Logstash Cloud Foundry App

A Logstash Cloud Foundry App has the following structure:

```
.
├── certificates
│   └── elasticsearch.crt
├── conf.d
│   ├── filter.conf
│   ├── input.conf
│   └── output.conf
├── curator.d
│   ├── actions.yml
│   └── curator.yml
├── grok-patterns
│   └── grok-patterns
├── plugins
│   └── logstash-output-kafka-7.0.4.gem
├── Logstash
└── manifest.yml
```

#### Logstash

The `Logstash` file in the root directory of the app is required. It is used by the buildpack to detect if the app is in fact a Logstash app. Furthermore it allows to configure the buildpack / the deployment of the app in yaml format.

The following settings are allowed:

* `logstash-credentials.username`: the username used for authenticating when sending messages to logstash (optional)
* `logstash-credentials.password`: the password used for authenticating when sending messages to logstash (optional)
* `certificates`: additional certificates to install (array of certificate names, without file extension). Defaults to none.
* `cmd-args`: Additional command line arguments for Logstash. Empty by default
* `config-check`: Shall we do a Logstash config test before startting Logtstash. Defaults to true.
* `config-templates`: Defines which config templates should be used (array). Defaults to none  
* `config.templates.name`: Name of a pre-defined config template
* `config.template.service-instance-name`: Service Instance Name to which should be connected 
* `curator`: Curator settings
* `curator.install`: Defines if Curator should be installed or not. Defaults to false.
* `curator.schedule`: Schedule for curator (when to run curator) in cron like syntax (https://godoc.org/github.com/robfig/cron). Format `second minute hour day_of_month month day_of_week`
* `enable-service-fallback`: In case there is no service binded to the app in automated mode: We will fallback to stdout. Defaults to false.
* `heap-percentage`: Percentage of memory (Total memory - reserved memory) which can be used by the heap memory: Default is 75
* `java-opts`: Additional java arguments. Empty by default 
* `log-level`: Log level, "Info" or "Debug". Defaults to "Info"
* `plugins`: additional plugins to install (array of plugin names). Defaults to none. If you are in a disconnected environment put the plugin binaries into the plugin folder.
* `reserved-memory`: Reserved memory in MB which should not be used by heap memory. Default is 300
* `version`: Version of Logstash to be deployed. Defaults to 6.0.0


##### Currently available templates:


```
cf-input-http:
- defines listening ports for http 
- default in automatic mode

cf-input-syslog:
- defines listening ports for tcp and udp 
- type syslog

cf-filter-syslog:
- prepares the logstash events according to the syslog standard RFC 5424
- connects to cf elasticsearch service-instance 
- default in automatic mode

cf-output-elasticsearch:
- connects to the elasticsearch service-instance 
- writes the logstash events to elasticsearch
- default in automatic mode

cf-output-stdout:
- writes the logstash events to standard output
```


#### Example `Logstash` file:

```
log-level: Info
version: 6.0.0
cmd-args: ""
java-opts: ""
reserved-memory: 300
heap-percentage: 75
config-check: true
enable-service-fallback: true
logstash-credentials:
  username: myUsername
  password: myPassword
config-templates:
- name: cf-input-http
- name: cf-filter-syslog
- name: cf-output-elasticsearch
  service-instance-name: my-elasticsearch
- name: cf-output-stdout
plugins:
- logstash-input-kafka
- logstash-output-kafka
certificates:
- elasticsearch
curator:
  install: true
  version: ""
  schedule: "0 5 2 * * *"
```


#### manifest.yml

This is the [Cloud Foundry application manifest](https://docs.cloudfoundry.org/devguide/deploy-apps/manifest.html) file which is used by `cf push`.

This file may be used to set the service binding


#### certificates folder

Put any additional required certificate in this folder. They will be added to the java cacert truststore used by logstash. You don't have to do further configuration in the Logsstash config files. 

#### conf.d folder
In the folder `conf.d` the [Logstash](https://www.elastic.co/guide/en/logstash/current/index.html) configuration is provided. The folder is optional. All files in this directory are used as part of the Logstash configuration.
Prior to the start of Logstash, all files in this directory are processed by [dockerize](https://github.com/jwilder/dockerize) as templates.
This allow to update the configuration files based on the environment variables provided by Cloud Foundry (e.g. VCAP_APPLICATION, VCAP_SERVICES).

The supported functions for the templates are documented in [dockerize - using templates](https://github.com/jwilder/dockerize/blob/master/README.md#using-templates)
and [golang - template](https://golang.org/pkg/text/template/).


#### curator.d folder

Configuration folder for [curator](https://www.elastic.co/guide/en/elasticsearch/client/curator/current/index.html) containing two files:

* `actions.yml`: General configuration of curator. For details see section [Configuration File](https://www.elastic.co/guide/en/elasticsearch/client/curator/current/configfile.html) in the official documentation.
* `curator.yml`: Definitions of the actions to be executed by curator. For details see section [Action File](https://www.elastic.co/guide/en/elasticsearch/client/curator/current/actionfile.html) in the official documentation.

Both files are processed with [dockerize](https://github.com/jwilder/dockerize). For details see above in the section about the folder `conf.d`.


#### grok-patterns (and other 3rd party configuration)

You may provide additional configuration files like grok-patterns or useragent regexes in additional directories. To provide the correct path within the Logstash configuration, it's suggested to set the paths by the template engine. Example (use all grok patterns in directory `grok-patterns`):

```
patterns_dir => "{{ .Env.HOME }}/grok-patterns"
```

#### plugins

Put any additional required plugin (*.gem or *.zip) in this folder. Also define them in the Logstash file. 


### Deploy App to Cloud Foundry

To deploy the Logstash app to Cloud Foundry using this buildpack, use the following command:

```
cf push -b https://github.com/swisscom/logstash-buildpack.git
```

After the successful upload of the application to Cloud Foundry, you may use a *user provided service* to ship the logs of your
application to your newly deployed Logstash application.

Create the log drain:

```
cf cups logstash-log-drain -l https://USERNAME:PASSWORD@URL-OF-LOGSTASH-INSTANCE
```

Bind the log drain to your app. You could optionally bind multiple apps to one log drain:

```
cf bind-service YOUR-CF-APP-NAME logstash-log-drain
```

Restage the app to pick up the newly bound service:

```
cf restage YOUR-CF-APP-NAME
```

You find more details in the [Cloud Foundry documentation](https://docs.cloudfoundry.org/devguide/services/log-management.html)

Alternatively the log drain may also be configured in your application manifest as described in chapter [Application Log Streaming](https://docs.cloudfoundry.org/services/app-log-streaming.html).

## Limitations

* This buildpack is only tested on Ubuntu based deployments.





### Building the Buildpack (for developers and cf admins

To build this buildpack, run the following command from the buildpack's directory:

1. Source the .envrc file in the buildpack directory.

   ```bash
   source .envrc
   ```
   To simplify the process in the future, install [direnv](https://direnv.net/) which will automatically source .envrc when you change directories.

1. Install buildpack-packager

    ```bash
    (cd src/go/vendor/github.com/cloudfoundry/libbuildpack/packager/buildpack-packager && go install)
    ```

1. Build the buildpack

    ```bash
    buildpack-packager [ --cached | --uncached ]
    ```

1. Use in Cloud Foundry

   Upload the buildpack to your Cloud Foundry and optionally specify it by name

    ```bash
    cf create-buildpack [BUILDPACK_NAME] [BUILDPACK_ZIP_FILE_PATH] 1
    cf push my_app [-b BUILDPACK_NAME]
    ```

### Testing (TODO)

Buildpacks use the [Cutlass](https://github.com/cloudfoundry/libbuildpack/cutlass) framework for running integration tests.

To test this buildpack, run the following command from the buildpack's directory:

1. Source the .envrc file in the buildpack directory.

   ```bash
   source .envrc
   ```
   To simplify the process in the future, install [direnv](https://direnv.net/) which will automatically source .envrc when you change directories.

1. Run unit tests

    ```bash
    ./scripts/unit.sh
    ```

1. Run integration tests

    ```bash
    ./scripts/integration.sh
    ```

More information can be found on Github [cutlass](https://github.com/cloudfoundry/libbuildpack/cutlass).


### Acknowledgements

Inspired by the [Heroku buildpack](https://github.com/heroku/heroku-buildpack-go) and the [Go(Lang) Buildpack](https://github.com/cloudfoundry/go-buildpack)

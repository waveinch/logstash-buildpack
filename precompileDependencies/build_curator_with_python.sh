#!/bin/bash
BUILDDIR=/home/vcap/app
CACHEDIR=/home/vcap/app/cache

CURL="/usr/bin/curl -s -L --retry 15 --retry-delay 2"

#verify supported current versions of the dependencies, see: https://www.elastic.co/guide/en/elasticsearch/client/curator/current/python-source.html

# List available releases: https://www.python.org/ftp/python/
PYTHON3_VERSION="3.6.5"
PYTHON3_URL="https://www.python.org/ftp/python/${PYTHON3_VERSION}/Python-${PYTHON3_VERSION}.tgz"
# List available releases: https://github.com/certifi/python-certifi/releases
CERTIFI_URL="https://github.com/certifi/python-certifi/archive/2018.01.18.tar.gz"
CERTIFI_TARGET="certifi-2018.01.18.tar.gz"
# List available releases: https://github.com/pallets/click/releases
CLICK_URL="https://github.com/pallets/click/archive/6.7.tar.gz"
CLICK_TARGET="click-6.7.tar.gz"
# List available releases: https://github.com/elastic/curator/releases
CURATOR_VERSION="5.5.1"
ELASTICSEARCH_CURATOR_URL="https://github.com/elastic/curator/archive/v${CURATOR_VERSION}.tar.gz"
ELASTICSEARCH_CURATOR_TARGET="elasticsearch-curator-${CURATOR_VERSION}.tar.gz"
# List available releases: https://github.com/elastic/elasticsearch-py/releases
ELASTICSEARCH_URL="https://github.com/elastic/elasticsearch-py/archive/5.5.2.tar.gz"
ELASTICSEARCH_TARGET="elasticsearch-5.5.2.tar.gz"
# List available releases: http://pyyaml.org/download/pyyaml/
PYYAML_URL="http://pyyaml.org/download/pyyaml/PyYAML-3.12.tar.gz"
PYYAML_TARGET="PyYAML-3.12.tar.gz"
# List available releases: https://github.com/shazow/urllib3/releases
URLLIB3_URL="https://github.com/shazow/urllib3/archive/1.22.tar.gz"
URLLIB3_TARGET="urllib3-1.22.tar.gz"
# List available releases: https://github.com/alecthomas/voluptuous/releases
VOLUPTUOUS_URL="https://github.com/alecthomas/voluptuous/archive/0.9.3.tar.gz"
VOLUPTUOUS_TARGET="voluptuous-0.9.3.tar.gz"

BUILDDIR=/home/vcap/deps/0/curator-${CURATOR_VERSION}

	echo "Download python"
    mkdir -p ${BUILDDIR}/python3
    mkdir -p ${CACHEDIR}/python3
    cd ${CACHEDIR}/python3
    ${CURL} ${PYTHON3_URL} | tar xzf -
    echo "Downloaded ${PYTHON3_URL}"

    # Detect # of CPUs so make jobs can be parallelized
    CPUS=`grep -c ^processor /proc/cpuinfo`

    pushd Python-${PYTHON3_VERSION}
      ./configure --prefix=${BUILDDIR}/python3
      make -j${CPUS}
      make install
    popd

    # PATH: /home/vcap/app/python3/bin
    # ${BUILDDIR}/python3/bin

    echo "Download curator and dependencies"
    mkdir -p ${BUILDDIR}/curator
    mkdir -p ${CACHEDIR}/curator
    echo "Download certifi"
    ${CURL} -o ${CACHEDIR}/curator/${CERTIFI_TARGET} ${CERTIFI_URL}
    echo "finished"

    echo "Download click"
    ${CURL} -o ${CACHEDIR}/curator/${CLICK_TARGET} ${CLICK_URL}
    echo "finished"

    echo "Download elasticsearch-curator"
    ${CURL} -o ${CACHEDIR}/curator/${ELASTICSEARCH_CURATOR_TARGET} ${ELASTICSEARCH_CURATOR_URL}
    echo "finished"

    echo "Download elsticsearch"
    ${CURL} -o ${CACHEDIR}/curator/${ELASTICSEARCH_TARGET} ${ELASTICSEARCH_URL}
    echo "finished"

    echo "Download PyYAML"
    ${CURL} -o ${CACHEDIR}/curator/${PYYAML_TARGET} ${PYYAML_URL}
    echo "finished"

    echo "Download urllib3"
    ${CURL} -o ${CACHEDIR}/curator/${URLLIB3_TARGET} ${URLLIB3_URL}
    echo "finished"

    echo "Download voluptuous"
    ${CURL} -o ${CACHEDIR}/curator/${VOLUPTUOUS_TARGET} ${VOLUPTUOUS_URL}
    echo "finished"

    echo "All dependancies should be in ${CACHEDIR}/curator"
    find ${CACHEDIR}/curator

	export PATH=${BUILDDIR}/python3/bin:$PATH
    # --no-index prevents contacting pypi to download packages
    # --find-links tells pip where to look for the dependancies
    ${BUILDDIR}/python3/bin/pip3 install --no-index --find-links ${CACHEDIR}/curator --install-option="--prefix=${BUILDDIR}/curator" 'elasticsearch-curator>=5.5.1'

    echo "Installed to ${BUILDDIR}/curator"
    find ${BUILDDIR}/curator
	cd ${BUILDDIR}
	
	echo "Create tarball curator-${CURATOR_VERSION}-python-${PYTHON_VERSION}.tar.gz"
	tar czf /home/vcap/app/public/curator-${CURATOR_VERSION}-python-${PYTHON3_VERSION}.tar.gz curator python3
	
	if [ ! -f /home/vcap/app/public/curator-${CURATOR_VERSION}-python-${PYTHON3_VERSION}.tar.gz ]; then
		echo "ERROR creating tarball !"
		return
	fi
	echo "tarball curator-${CURATOR_VERSION}-python-${PYTHON3_VERSION} created !!"
	
	
	
	
	

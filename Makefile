PACKAGE_NAME:=$(shell cat deb.json | jq -r '.name')
VERSION:=$(shell cat deb.json | jq -r '.version')
ARCH?=amd64

all: test build

test:
	go-bin-deb test

deb:
	ARCH=amd64 make ${PACKAGE_NAME}_amd64.deb
	ARCH=arm64 make ${PACKAGE_NAME}_arm64.deb

${PACKAGE_NAME}_${ARCH}.deb: ${PACKAGE_NAME}_${VERSION}_${ARCH}.deb
${PACKAGE_NAME}_${VERSION}_${ARCH}.deb: deb.json systemd/* $(APP_RESOURCES) build/${ARCH}/cgroup-exporter
	rm -rf pkg-build
	go-bin-deb generate --arch ${ARCH}

clean:
	rm -rf build
	rm -rf pkg-build
	rm -f ${PACKAGE_NAME}_*.deb

install:
	sudo dpkg -i ${PACKAGE_NAME}_${VERSION}_${ARCH}.deb

uninstall:
	sudo dpkg -r ${PACKAGE_NAME}

build: build/${ARCH}/cgroup-exporter
build/${ARCH}/cgroup-exporter: deb.json
	go get -d -t .
	@rm -rf build/${ARCH} && mkdir -p build/${ARCH}
	GOARCH=${ARCH} go build -o $@ -ldflags "-X main.version=${VERSION} -X main.git=${GIT_HASH}" .


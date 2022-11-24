katzen
=======

[![Build Status](https://github.com/katzenpost/katzen/actions/workflows/go.yml/badge.svg?branch=main)](https://github.com/katzenpost/katzen/actions/workflows/go.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/katzenpost/katzen)](https://goreportcard.com/report/github.com/katzenpost/katzen)
[![GoDoc](https://godoc.org/github.com/golang/gddo?status.svg)](https://pkg.go.dev/github.com/katzenpost/katzen?tab=doc)

A multiplatform chat client using catshadow and gio

## Getting the source code

    git clone https://github.com/katzenpost/katzen
    cd katzen

## Building katzen for GNU/Linux

### Using docker or podman

The easiest way to build katzen is in a container managed by
[podman](https://podman.io/) or
[docker](https://en.wikipedia.org/wiki/Docker_(software)). (Podman is a docker
replacement which has the advantages of not requiring root access or a daemon to
operate.)

The Makefile in this repository has targets to create a container image with
the build environment, and to create a container with that image, and build
katzen in the container.

Although the make target names are prefixed with `docker-`, podman will be used
by default if it is installed. If you have both podman and docker installed
and want to force the use of docker, or if you want to specify the path to a
different docker/podman compatible binary, you can specify `docker=docker` as
the first argument to the `make` command.

The default target is `docker-build-linux` which will produce a linux binary.
Other Makefile targets include `docker-build-windows` and
`docker-build-android`, which build for those platforms, and `docker-shell`
which puts you in a shell inside an ephemeral build container with your local
checkout mounted at `/go/katzen`.

To remove the docker images and containers created by the Makefile, run `make
docker-clean`. The Makefile contains targets which build intermediate images
for the Debian and go module dependencies, so that local changes can be built
without the need for internet access.

To build using local uncomitted changes from the Katzenpost monorepo, add
`replace github.com/katzenpost/katzenpost => ./katzenpost` to katzen's `go.mod`
file and clone the monorepo in your katzen checkout. (Note: this workflow is
currently hampered by the need to manually build the libsphincsplus.a library
in the local checkout of katzenpost.)

### Building without docker

Make sure you have a working Go environment (Go 1.16 or higher is required; on
Debian buster the backports repository can be used).

See the [install instructions](http://golang.org/doc/install.html).

#### Installing golang (Debian Bullseye example)

    apt install golang git ca-certificates
    export GOPATH=$HOME/go

#### Install debian dependencies (Debian Bullseye example)

    apt install --no-install-recommends build-essential libgles2 libgles2-mesa-dev libglib2.0-dev libxkbcommon-dev libxkbcommon-x11-dev libglu1-mesa-dev libxcursor-dev libwayland-dev libx11-xcb-dev libvulkan-dev

#### Download and verify dependencies

   go mod download && go mod verify

#### Build katzen

    go build -trimpath -ldflags=-buildid=

# Cross-compilation dependencies for the arm64 architecture

    dpkg --add-architecture arm64 && apt update
    apt install --no-install-recommends crossbuild-essential-arm64 libgles2:arm64 libgles2-mesa-dev:arm64 libglib2.0-dev:arm64 libxkbcommon-dev libxkbcommon-x11-dev:arm64 libglu1-mesa-dev:arm64 libxcursor-dev:arm64 libwayland-dev:arm64 libx11-xcb-dev:arm64 libvulkan-dev:arm64

#### Building for arm64

    CC=aarch64-linux-gnu-gcc CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags=-buildid=

#### Building for Windows

    GOOS=windows go build -trimpath -ldflags="-H windowsgui -buildid="

#### Building for macOS (Intel), requires macOS and xcode

    GOARCH="amd64" go build -trimpath -ldflags=-buildid=

#### Building for macOS (Apple M1, arm), requires macOS 11 and xcode

    CGO_ENABLED=1 GOOS="darwin" GOARCH="arm64" go build -tags dynamic -trimpath -ldflags=-buildid=

#### Building for android

Note that you will need to have the android NDK and SDK installed and the
appropriate environment variables exported.

See the Dockerfile.android in this repository to set up a build environment if you wish.

First, get and install the gogio tool:

    go install gioui.org/cmd/gogio

Generate an Android signing key so you can update your app later:
(keytool is provided by the openjdk package: apt install openjdk-11-jdk, or use within the docker container)

    keytool -genkey -keystore sign.keystore -storepass YOURPASSWORD -alias android -keyalg RSA -keysize 2048 -validity 10000 -noprompt -dname CN=android

And then build the Android APK:

    gogio -arch arm64,amd64 -x -target android -appid chat.katzen -version 1 -signkey sign.keystore -signpass YOURPASSWORD .

To create the Docker build environment:

    docker build --no-cache -t katzenpost/android_build -f Dockerfile.android .

Optional: Before using the build environment, you can `go mod vendor` to copy
dependencies into a local path (`./vendor`).  This will allow you to use go.mod
replace directives and copy cached go modules so that they will be available in
the docker container. This will speed up builds and enable you to make local
changes to test on android. Do not forget to update or remove the vendor path.

To run the build, execute the following command while in this project root:

    docker run -v "$(pwd)":/go/build/ katzenpost/android_build bash -c "go install gioui.org/cmd/gogio && gogio -arch arm64,amd64 -x -target android -appid org.mixnetworks.katzen -version 1 -signkey sign.keystore -signpass YOURPASSWORD ."

Note that the contents of the local directory are copied into the docker environment - so your signing keystore ought to be in this path as well.

To install on an Android device using `adb` run the following

    adb install katzen.apk

Between versions you might need to install uninstall a previous build

    adb uninstall chat.katzen

#### Verify build: To verify that your local build matches the CI-built apk, use the included reproducible.keystore to sign the apk:

    docker run --rm -v "$(pwd)":/go/build/ katzenpost/android_build bash -c "go install gioui.org/cmd/gogio && gogio -arch arm64,amd64 -x -target android -appid org.mixnetworks.katzen -version 1 -signkey reproducible.keystore -signpass reproducible ."

## Run it

    Usage of ./deploy/linux/katzen:
      -f string
         Path to the client config file. (default to baked-in testnet configuration)
      -s string
         The catshadow state file path. (default "catshadow_statefile")

## supported by

[![NGI](https://katzenpost.mixnetworks.org/_static/images/eu-flag-tiny.jpg)](https://www.ngi.eu/about/)
<a href="https://nlnet.nl"><img src="https://nlnet.nl/logo/banner.svg" width="160" alt="NLnet Foundation"/></a>
<a href="https://nlnet.nl/assure"><img src="https://nlnet.nl/image/logos/NGIAssure_tag.svg" width="160" alt="NGI Assure"/></a>
<a href="https://nlnet.nl/NGI0"><img src="https://nlnet.nl/image/logos/NGI0PET_tag.svg" width="160" alt="NGI Zero PET"/></a>

This project has received funding from:

* European Union’s Horizon 2020 research and innovation programme under the Grant Agreement No 653497, Privacy and Accountability in Networks via Optimized Randomized Mix-nets (Panoramix).
* The Samsung Next Stack Zero grant.
* NGI0 PET Fund, a fund established by NLnet with financial support from the European Commission's Next Generation Internet programme, under the aegis of DG Communications Networks, Content and Technology under grant agreement No 825310.
* NGI Assure Fund, a fund established by NLnet with financial support from the European Commission's Next Generation Internet programme, under the aegis of DG Communications Networks, Content and Technology under grant agreement No 957073.

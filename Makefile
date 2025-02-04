docker := $(shell if which podman|grep -q .; then echo podman; else echo docker; fi)
warped?=false
ldflags=-buildid= -X github.com/katzenpost/katzenpost/core/epochtime.WarpedEpoch=${warped} -X github.com/katzenpost/katzenpost/server/internal/pki.WarpedEpoch=${warped} -X github.com/katzenpost/katzenpost/minclient/pki.WarpedEpoch=${warped}
KEYSTORE := sign.keystore
KEYPASS := password
# gogio requires a version string ("%d.%d.%d.%d", &sv.Major, &sv.Minor, &sv.Patch, &sv.VersionCode)
# this is katzen v1 with katzenpost v0.0.35
VERSION := 1.35.0
# this is the app store application version code that must incrememnt with each official release
VERSIONCODE := 1
cache_dir=cache
# you can say, eg, 'make go_package_cache_arg= docker-shell' to not use the package cache
go_package_cache_arg := -v $(shell readlink -f .)/$(cache_dir)/go:/go/ -e GOCACHE=/go/cache
docker_run_cmd=run --rm -v "$(shell readlink -f .)":/go/katzen/ $(go_package_cache_arg) --workdir /go/katzen -e CGO_CFLAGS_ALLOW="-DPARAMS=sphincs-shake-256f"

distro=debian

$(cache_dir): $(cache_dir)/go

$(cache_dir)/go:
	mkdir -p $(cache_dir)/go

docker-test: docker-$(distro)-base
	$(docker) $(docker_run_cmd) --rm katzen/$(distro)_base \
		go test -coverprofile=coverage.out -race -v -failfast -timeout 30m ./...

docker-build-linux: docker-$(distro)-base
	@([ "$(distro)" = "debian" ] || [ "$(distro)" = "alpine" ]) || \
		(echo "can only docker-build-linux for debian or alpine, not $(distro)" && false)
	$(docker) $(docker_run_cmd) katzen/$(distro)_base go build -trimpath -ldflags="${ldflags}"

docker-build-windows: docker-debian-base
	@if [ "$(distro)" != "debian" ]; then \
		echo "can only docker-build-windows on debian"; \
		false; \
	fi
	$(docker) $(docker_run_cmd) katzen/$(distro)_base bash -c 'cd /go/katzen/; HIGHCTIDH_PORTABLE=1 GOOS=windows CGO_ENABLED=1 CGO_LDFLAGS="-Wl,--no-as-needed -Wl,-allow-multiple-definition" CC=x86_64-w64-mingw32-gcc go build -trimpath -ldflags="-H windowsgui ${ldflags}" -o katzen.exe'

docker-android-base:
	if ! $(docker) images|grep katzen/android_sdk; then \
		$(docker) build -t katzen/android_sdk -f Dockerfile.android .; \
	fi

$(KEYSTORE):
	$(docker) $(docker_run_cmd) katzen/android_sdk bash -c "keytool -genkey -keystore $(KEYSTORE) -storepass ${KEYPASS} -alias android -keyalg RSA -keysize 2048 -validity 10000 -noprompt -dname CN=android"

docker-build-android: $(cache_dir) docker-android-base $(KEYSTORE)
	@if [ "$(distro)" != "debian" ]; then \
		echo "can only docker-build-android on debian"; \
		false; \
	fi
	$(docker) $(docker_run_cmd) katzen/android_sdk bash -c "cd replace-gogio && go install gioui.org/cmd/gogio && cd .. && gogio -arch arm64,amd64 -x -target android -appid chat.katzen -version $(VERSION).$(VERSIONCODE) -signkey $(KEYSTORE) -signpass ${KEYPASS} ."

# this builds the debian base image, ready to have the golang deps installed
docker-debian-base: $(cache_dir)
	if ! $(docker) images|grep katzen/debian_base; then \
		$(docker) run --replace --name katzen_debian_base docker.io/golang:bookworm bash -c "echo -e 'deb https://deb.debian.org/debian bookworm main\ndeb https://deb.debian.org/debian bookworm-updates main\ndeb https://deb.debian.org/debian-security bookworm-security main' > /etc/apt/sources.list && cat /etc/apt/sources.list && apt update && apt upgrade -y && apt install -y --no-install-recommends build-essential libgles2 libgles2-mesa-dev libglib2.0-dev libxkbcommon-dev libxkbcommon-x11-dev libglu1-mesa-dev libxcursor-dev libwayland-dev libx11-xcb-dev libvulkan-dev gcc-mingw-w64-x86-64" \
		&& $(docker) commit katzen_debian_base katzen/debian_base \
		&& $(docker) rm katzen_debian_base; \
	fi

docker-nix-base.stamp: $(cache_dir)
	$(docker) run --replace --name katzen_nix_base \
		-v "$(shell readlink -f .)":/katzen/ --workdir /katzen \
		docker.io/nixos/nix:master nix \
		--extra-experimental-features flakes \
		--extra-experimental-features nix-command \
		develop --command true \
		&& $(docker) commit katzen_nix_base katzen/nix_base \
		&& $(docker) rm katzen_nix_base
		touch $@

docker-nix-flake-update: docker-nix-base.stamp
	$(docker) pull docker.io/nixos/nix:master
	$(docker) run --rm -v "$(shell readlink -f .)":/katzen/ --workdir /katzen \
		docker.io/nixos/nix:master nix \
		--extra-experimental-features flakes \
		--extra-experimental-features nix-command \
		flake update -L

docker-build-nix: docker-nix-base.stamp
	# this is for testing and updating the vendorHash (manually, after running go mod...).
	# actual nix users should see README (FIXME put nix command in README)
	@mkdir -p nix_build
	@$(docker) $(docker_run_cmd) --rm -it katzen/nix_base \
		bash -c ' \
			nix --extra-experimental-features flakes \
				--extra-experimental-features nix-command \
				build . -L \
			&& cp -rp $$(readlink result) nix_build/'

docker-alpine-base: $(cache_dir)
	@if ! $(docker) images|grep katzen/alpine_base; then \
		$(docker) run --replace --name katzen_alpine_base docker.io/golang:alpine \
		sh -c 'apk add bash gcc musl-dev libxkbcommon-dev pkgconf wayland-dev \
					   vulkan-headers mesa-dev libx11-dev libxcursor-dev' \
		&& $(docker) commit katzen_alpine_base katzen/alpine_base \
		&& $(docker) rm katzen_alpine_base; \
	fi

docker-go-mod-go-get: docker-$(distro)-base
	$(docker) $(docker_run_cmd) --rm katzen/$(distro)_base \
			bash -c 'cd /go/katzen; go get'

docker-go-mod-update: docker-$(distro)-base
	$(docker) $(docker_run_cmd) --rm katzen/$(distro)_base \
			bash -c 'cd /go/katzen; go mod tidy -compat=1.19' 

# This will upgrade all of katzen's dependency pins, and modify go.mod and
# go.sum accordingly.
docker-go-mod-upgrade: docker-$(distro)-base
	$(docker) $(docker_run_cmd) --rm katzen/$(distro)_base \
			bash -c 'cd /go/katzen; go get -d -u'

docker-shell: docker-$(distro)-base
	$(docker) $(docker_run_cmd) --rm -it katzen/$(distro)_base bash

docker-android-shell: docker-android-base
	$(docker) $(docker_run_cmd) --rm -it katzen/android_sdk bash

docker-clean:
	-rm -vf result
	-rm -rvf nix_build
	-rm -rvf $(cache_dir)
	-rm -rvf ./go_package_cache # for users of old versions of this makefile
	-rm -fv *.stamp
	-$(docker) rm  katzen_debian_base
	-$(docker) rm  katzen_alpine_base
	-$(docker) rm  katzen_nix_base
	-$(docker) rmi katzen/$(distro)_base katzen/android_sdk


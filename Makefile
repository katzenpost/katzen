docker := $(shell if which podman|grep -q .; then echo podman; else echo docker; fi)
warped?=false
ldflags=-buildid= -X github.com/katzenpost/katzenpost/core/epochtime.WarpedEpoch=${warped} -X github.com/katzenpost/katzenpost/server/internal/pki.WarpedEpoch=${warped} -X github.com/katzenpost/katzenpost/minclient/pki.WarpedEpoch=${warped}
KEYSTORE := sign.keystore
KEYPASS := password

docker-build-linux: docker-go-mod
	$(docker) run --rm -v "$(shell readlink -f .)":/go/katzen/ katzen/go_mod bash -c 'cd /go/katzen/; CGO_CFLAGS_ALLOW="-DPARAMS=sphincs-shake-256f" go build -trimpath -ldflags="${ldflags}"'

docker-build-windows: docker-go-mod
	$(docker) run --rm -v "$(shell readlink -f .)":/go/katzen/ katzen/go_mod bash -c 'cd /go/katzen/; GOOS=windows CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc CGO_CFLAGS_ALLOW="-DPARAMS=sphincs-shake-256f" go build -trimpath -ldflags="-H windowsgui ${ldflags}" -o katzen.exe'

docker-android-base:
	if ! $(docker) images|grep katzen/android_sdk; then \
		$(docker) build --no-cache -t katzen/android_sdk -f Dockerfile.android .; \
	fi

android-signing-key: docker-android-base
	if [ ! -e $(KEYSTORE) ]; then \
		$(docker) run --rm -v "$(shell readlink -f .)":/go/build katzen/android_sdk bash -c "keytool -genkey -keystore $(KEYSTORE) -storepass ${KEYPASS} -alias android -keyalg RSA -keysize 2048 -validity 10000 -noprompt -dname CN=android"; \
	fi

docker-build-android: android-signing-key
	$(docker) run --rm -v "$(shell readlink -f .)":/go/build katzen/android_sdk bash -c "go install gioui.org/cmd/gogio && CGO_CFLAGS_ALLOW="-DPARAMS=sphincs-shake-256f" gogio -arch arm64,amd64 -x -target android -appid org.mixnetworks.katzen -version 1 -signkey $(KEYSTORE) -signpass ${KEYPASS} ."

# this builds the debian base image, ready to have the golang deps installed
docker-debian-base:
	if ! $(docker) images|grep katzen/debian_base; then \
		$(docker) run --name katzen_debian_base docker.io/golang:bullseye bash -c "echo 'deb https://deb.debian.org/debian bullseye main\ndeb https://deb.debian.org/debian bullseye-updates main\ndeb https://deb.debian.org/debian-security bullseye-security main' > /etc/apt/sources.list && cat /etc/apt/sources.list && apt update && apt upgrade -y && apt install -y --no-install-recommends build-essential libgles2 libgles2-mesa-dev libglib2.0-dev libxkbcommon-dev libxkbcommon-x11-dev libglu1-mesa-dev libxcursor-dev libwayland-dev libx11-xcb-dev libvulkan-dev gcc-mingw-w64-x86-64" \
		&& $(docker) commit katzen_debian_base katzen/debian_base \
		&& $(docker) rm katzen_debian_base; \
	fi

# this is the image with all golang deps installed, ready to build katzen
docker-go-mod: docker-debian-base
	if ! $(docker) images|grep katzen/go_mod; then \
		$(docker) run -v "$(shell readlink -f .)":/go/katzen --workdir /go/katzen \
            --name katzen_go_mod katzen/debian_base \
			bash -c 'go get && go mod verify' \
		&& $(docker) commit katzen_go_mod katzen/go_mod \
		&& $(docker) rm katzen_go_mod; \
	fi

# this will re-run go get, and save a new katzen/go_mod image
# this is for running after manually editing go.mod
docker-go-mod-go-get: docker-go-mod
	$(docker) run -v "$(shell readlink -f .)":/go/katzen --name katzen_go_mod katzen/go_mod \
			bash -c 'cd /go/katzen; go get' \
		&& $(docker) commit katzen_go_mod katzen/go_mod \
		&& $(docker) rm katzen_go_mod

# this will run go mod tidy, and save a new katzen/go_mod image
# this is for running after manually editing go.mod, and will update go.mod and
# go.sum to reflect all of the indirect dependency changes required by the
# manual change.
docker-go-mod-update: docker-go-mod
	$(docker) run -v "$(shell readlink -f .)":/go/katzen --name katzen_go_mod katzen/go_mod \
			bash -c 'cd /go/katzen; go mod tidy -compat=1.19' \
		&& $(docker) commit katzen_go_mod katzen/go_mod \
		&& $(docker) rm katzen_go_mod

# this will run go get, and save a new katzen/go_mod image
# This will upgrade all of katzen's dependency pins, and modify go.mod and
# go.sum accordingly.
docker-go-mod-upgrade: docker-go-mod
	$(docker) run -v "$(shell readlink -f .)":/go/katzen --name katzen_go_mod katzen/go_mod \
			bash -c 'cd /go/katzen; go get -d -u' \
		&& $(docker) commit katzen_go_mod katzen/go_mod \
		&& $(docker) rm katzen_go_mod

docker-shell: docker-go-mod
	$(docker) run -v "$(shell readlink -f .)":/go/katzen --rm -it katzen/go_mod bash

docker-android-shell: docker-android-base
	$(docker) run -v "$(shell readlink -f .)":/go/build --rm -it katzen/android_sdk bash

docker-clean:
	$(docker) rm  katzen_debian_base katzen_go_mod || true
	$(docker) rmi katzen/debian_base katzen/go_mod katzen/android_sdk || true

docker-clean-go-mod:
	$(docker) rm katzen_go_mod || true
	$(docker) rmi katzen/go_mod || true

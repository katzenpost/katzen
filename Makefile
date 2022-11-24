docker := docker

docker-build-linux: docker-go-mod
	$(docker) run --rm -v "$(shell readlink -f .)":/go/katzen/ katzen/go_mod bash -c 'cd /go/katzen/; go build -trimpath -ldflags=-buildid='

docker-build-windows: docker-go-mod
	$(docker) run --rm -v "$(shell readlink -f .)":/go/katzen/ katzen/go_mod bash -c 'cd /go/katzen/; GOOS=windows go build -trimpath -ldflags="-H windowsgui -buildid=" -o katzen.exe'

docker-debian-base:
	if ! $(docker) images|grep katzen/debian_base; then \
		$(docker) run --name katzen_debian_base golang:bullseye bash -c 'apt update && apt upgrade -y && apt install -y --no-install-recommends build-essential libgles2 libgles2-mesa-dev libglib2.0-dev libxkbcommon-dev libxkbcommon-x11-dev libglu1-mesa-dev libxcursor-dev libwayland-dev libx11-xcb-dev libvulkan-dev' \
		&& $(docker) commit katzen_debian_base katzen/debian_base \
		&& $(docker) rm katzen_debian_base; \
	fi

docker-go-mod: docker-debian-base
	if ! $(docker) images|grep katzen/go_mod; then \
		$(docker) run -v "$(shell readlink -f .)":/go/katzen --name katzen_go_mod katzen/debian_base \
			bash -c 'cd /go/katzen; go mod tidy -compat=1.17' \
		&& $(docker) commit katzen_go_mod katzen/go_mod \
		&& $(docker) rm katzen_go_mod; \
	fi

docker-go-mod-update: docker-go-mod
	$(docker) run -v "$(shell readlink -f .)":/go/katzen --name katzen_go_mod katzen/go_mod \
			bash -c 'cd /go/katzen; go mod tidy -compat=1.17' \
		&& $(docker) commit katzen_go_mod katzen/go_mod \
		&& $(docker) rm katzen_go_mod

docker-go-mod-upgrade: docker-go-mod
	$(docker) run -v "$(shell readlink -f .)":/go/katzen --name katzen_go_mod katzen/go_mod \
			bash -c 'cd /go/katzen; go get -d -u' \
		&& $(docker) commit katzen_go_mod katzen/go_mod \
		&& $(docker) rm katzen_go_mod

docker-android-base:
	if ! $(docker) images|grep katzen/android_build; then \
		$(docker) build --no-cache -t katzenpost/android_build -f Dockerfile.android . ; \
	fi

docker-build-android: docker-android-base
	$(docker) run -v "$(shell readlink -f .)":/go/build/ katzen/android_build bash -c "go install gioui.org/cmd/gogio && gogio -arch arm64,amd64 -x -target android -appid chat.katzen -version 1 -signkey reproducible.keystore -signpass reproducible ."

docker-shell: docker-debian-base
	$(docker) run -v "$(shell readlink -f .)":/go/katzen --rm -it katzen/debian_base bash

docker-android-shell: docker-android-base
	$(docker) run -v "$(shell readlink -f .)":/go/build --rm -it katzen/android_build bash

docker-clean:
	$(docker) rm  katzen_debian_base katzen_go_mod || true
	$(docker) rmi katzen/debian_base katzen/go_mod || true

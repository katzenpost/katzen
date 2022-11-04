docker-build-linux: docker-go-mod
	docker run --rm -v "$(shell readlink -f .)":/go/katzen/ katzen/go_mod bash -c 'cd /go/katzen/; go build -trimpath -ldflags=-buildid='

docker-build-windows: docker-go-mod
	docker run --rm -v "$(shell readlink -f .)":/go/katzen/ katzen/go_mod bash -c 'cd /go/katzen/; GOOS=windows go build -trimpath -ldflags="-H windowsgui -buildid=" -o katzen.exe'

docker-debian-base:
	if ! docker images|grep katzen/debian_base; then \
		docker run --volume /usr/local/lib/libsphincsplus.so:/libsphincsplus.so --volume /usr/local/include/sphincsplus:/sphincsplusinclude --name katzen_debian_base golang:bullseye bash -c 'apt update && apt upgrade -y && apt install -y --no-install-recommends build-essential libgles2 libgles2-mesa-dev libglib2.0-dev libxkbcommon-dev libxkbcommon-x11-dev libglu1-mesa-dev libxcursor-dev libwayland-dev libx11-xcb-dev libvulkan-dev && cp /libsphincsplus.so /usr/local/lib && ldconfig && cp -a /sphincsplusinclude /usr/local/include/sphincsplus' \
		&& docker commit katzen_debian_base katzen/debian_base \
		&& docker rm katzen_debian_base; \
	fi

docker-go-mod: docker-debian-base
	if ! docker images|grep katzen/go_mod; then \
		docker run -v "$(shell readlink -f .)":/go/katzen --name katzen_go_mod katzen/debian_base \
			bash -c 'cd /go/katzen; go mod tidy' \
		&& docker commit katzen_go_mod katzen/go_mod \
		&& docker rm katzen_go_mod; \
	fi

docker-go-mod-update: docker-go-mod
	docker run -v "$(shell readlink -f .)":/go/katzen --name katzen_go_mod katzen/go_mod \
			bash -c 'cd /go/katzen; go mod tidy' \
		&& docker commit katzen_go_mod katzen/go_mod \
		&& docker rm katzen_go_mod

docker-go-mod-upgrade: docker-go-mod
	docker run -v "$(shell readlink -f .)":/go/katzen --name katzen_go_mod katzen/go_mod \
			bash -c 'cd /go/katzen; go get -d -u' \
		&& docker commit katzen_go_mod katzen/go_mod \
		&& docker rm katzen_go_mod

docker-shell: docker-debian-base
	docker run -v "$(shell readlink -f .)":/go/katzen --rm -it katzen/debian_base bash

docker-clean:
	docker rm  katzen_debian_base katzen_go_mod || true
	docker rmi katzen/debian_base katzen/go_mod || true

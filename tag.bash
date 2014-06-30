#!/usr/bin/env sh

TAG_NAME="$1"

if [ -z "$TAG_NAME" ]; then
    echo 1>&2 'no tag name given'
    exit 1
fi

build() {
    os=$1
    arch=$2
    echo $os $arch
    GOOS=$os GOARCH=$arch go build && tar czf "jqsh${TAG_NAME}.${os}-${arch}.tar.gz" jqsh && rm jqsh
    [ $? -eq 0 ] || exit 1
}

if [ -n "`git status --porcelain`" ]; then
    echo 1>&2 'repository is not clean'
    exit 1
fi


if [ -n "`ls *.tar.gz 2>/dev/null`" ]; then
    echo 1>&2 'remove previous distribution files'
    exit 1
fi

git tag -a "$1"

build darwin amd64
build linux amd64

echo "all distributions built successfully"

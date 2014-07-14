#!/usr/bin/env sh

TAG_NAME="$1"

# NEXT_NAME is the next version number to use.  The name is a little misleading
# because it will probably have a suffix like "dev".  And there will never be a
# tag with that suffix.
NEXT_NAME="$2"

if [ -z "$TAG_NAME" ]; then
    echo 1>&2 'no tag name given'
    exit 1
fi

if [ -z "$NEXT_NAME" ]; then
    echo 1>&2 'no next tag given'
    exit 1
fi

version_go() {
cat <<EOGO
// WARNING: this file is generated do not modify it manually.
package main

const Version = "$1"
EOGO
}

build() {
    os=$1
    arch=$2
    tarfile="jqsh${TAG_NAME}.${os}-${arch}.tar.gz"
    echo "$tarfile"
    GOOS=$os GOARCH=$arch go build && tar czf "$tarfile" jqsh && rm jqsh
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

version_go "$TAG_NAME" > version.go
git add version.go
git commit -m "bump version to $TAG_NAME"

git tag -a "$1"

build darwin amd64
build linux amd64
md5 jqsh"${TAG_NAME}".*.tar.gz > jqsh"${TAG_NAME}".md5

echo "all distributions built successfully"

version_go "$NEXT_NAME" > version.go
git add version.go
git commit -m "bump version to $NEXT_NAME"

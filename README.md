#jqsh

An interactive wrapper to the [jq](http://stedolan.github.io/jq/) command line utility.

##Install

Download an archive for your system from the [release
page](https://github.com/bmatsuo/jqsh/releases) and extract the binary somwhere
in your PATH. For example, "/usr/bin".

    sudo tar -C /usr/bin -xvzf jqsh0.2.darwin-amd64.tar.gz

**NOTE**: The above command varies depending on your system and the current
version of jqsh.

If Go is installed on your system, you can install the unstable HEAD using `go
get`.

    go get github.com/bmatsuo/jqsh

##Getting started

Reference documentation can be found on
[godoc.org](http://godoc.org/github.com/bmatsuo/jqsh).

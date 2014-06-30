#jqsh

An interactive wrapper to the [jq](http://stedolan.github.io/jq/) command line utility.

##Install

Download a [binary distribution](https://github.com/bmatsuo/jqsh/releases) for
your system  and extract it into your PATH (for example, in "/usr/bin").

    sudo tar -C /usr/bin -xvzf jqsh0.2.darwin-amd64.tar.gz

**NOTE**: The above command varies depending on your system and the current
version of jqsh.

If Go is installed on your system, you can install the latest (unstable)
version of jqsh using `go get`.

    go get -u github.com/bmatsuo/jqsh

##Getting started

Reference documentation can be found on
[godoc.org](http://godoc.org/github.com/bmatsuo/jqsh).

##Readline

Jqsh does not have builtin support for readline or other fancy line editing.
The rlwrap program can be used instead to provide line editing and history.

    rlwrap -A -N jqsh

##License

Copyright (c) 2014 Bryan Matsuo

Jqsh is distributed under the MIT open source license.  A copy of the license
agreement can be located in the LICENSE file.

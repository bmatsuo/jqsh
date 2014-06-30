#jqsh [![Build Status](https://travis-ci.org/bmatsuo/jqsh.svg?branch=master)](https://travis-ci.org/bmatsuo/jqsh)

An interactive wrapper to the [jq](http://stedolan.github.io/jq/) command line utility.

##Install

Download a [binary distribution](https://github.com/bmatsuo/jqsh/releases) for
your system  and extract it into your PATH (for example, in "/usr/bin").  Be
aware **the installation command below varies** depending on your system and
the current version of jqsh (it is for jqsh0.3 on OS X).

    $ sudo tar -C /usr/bin -xvzf jqsh0.3.darwin-amd64.tar.gz

If Go is installed on your system, you can instead compile the latest
(unstable) version of jqsh.

    $ go get -u github.com/bmatsuo/jqsh

##Getting started

    $ jqsh
    > :help

Reference documentation is on
[godoc.org](http://godoc.org/github.com/bmatsuo/jqsh) for now.

##Readline

Jqsh does not have builtin support for readline or other fancy line editing.
Use [rlwrap](http://utopia.knoware.nl/~hlub/rlwrap/#rlwrap) for now.

    $ rlwrap -A -N jqsh

The above command should work on Linux (untested) and [OS
X](https://github.com/bmatsuo/jqsh/issues/3#issuecomment-47522319).

##License

Copyright (c) 2014 Bryan Matsuo

Jqsh is distributed under the MIT open source license.  A copy of the license
agreement can be located in the LICENSE file.

#jqsh [![Build Status](https://travis-ci.org/bmatsuo/jqsh.svg?branch=master)](https://travis-ci.org/bmatsuo/jqsh)

An interactive wrapper to the [jq](http://stedolan.github.io/jq/) command line utility.

##Install

Download a [binary distribution](https://github.com/bmatsuo/jqsh/releases) for
your system  and extract it into your PATH (for example, in "/usr/bin").  Be
aware **the installation command below varies** depending on your system and
the current version of jqsh (it is for jqsh0.4 on OS X).

    $ sudo tar -C /usr/bin -xvzf jqsh0.4.darwin-amd64.tar.gz

If Go is installed on your system, you can instead compile the latest
(unstable) version of jqsh.

    $ go get -u github.com/bmatsuo/jqsh

**NOTE (Windows users):** I have no reason to think jqsh wouldn't build or work
on Windows but I don't test on Windows and thus don't provide Windows
executables. Feel free to [contribute](#contributing) patches for windows
compatibility. But, until farther along in the future Windows will remain
unofficially supported at best.

##Getting started

Here's an example jqsh session to get a feel for how the shell works.  It's
good to have some background working with
[jq](http://stedolan.github.io/jq/manual/).  Although it's straight forward
enough to not be necessary.

    $ jqsh example.json
    > .items[].name
    > :pop
    > .items[]
    > select(.message | contains("hello"))
    > :quit

Reference documentation is on
[godoc.org](http://godoc.org/github.com/bmatsuo/jqsh) for now.

##Readline

Jqsh lacks advanced editing and history support.
[Rlwrap](http://utopia.knoware.nl/~hlub/rlwrap/#rlwrap) can help until support
arrives.

    $ rlwrap -A -N jqsh

The above command should work on Linux (untested) and [OS
X](https://github.com/bmatsuo/jqsh/issues/3#issuecomment-47522319).

##Troubleshooting

If you run into bugs or confusing behavior first update to the latest release.
If the behavior persists [search for related
issues](https://github.com/bmatsuo/jqsh/search?ref=cmdform&type=Issues).

As a last resort, [open an issue](https://github.com/bmatsuo/jqsh/issues/new).
Be as descriptive as possible about your system and the situation in which you
experience undesirable behavior.

##Contributing

Everyone is encouraged to [make
suggestions](https://github.com/bmatsuo/jqsh/issues/new) regarding how they
think jqsh could be a better utility.  But I'd like to keep development fairly
regulated in these early stages.  Jqsh is an awesome utility and things could
go in a bunch of directions. I just want to make sure it is guided properly and
don't want to make big decisions hastily.

If you want to work on something, comment on the issue so that people don't end
up worknig on the same thing by accident.

##License

Copyright (c) 2014 Bryan Matsuo

Jqsh is distributed under the MIT open source license.  A copy of the license
agreement can be located in the LICENSE file.

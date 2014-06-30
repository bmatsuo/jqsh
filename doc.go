/*
Command jqsh provides an interactive wrapper to the jq command line utility.

Shell syntax

The current shell syntax is rudimentory but it suffices.  Commands are prefixed
with a color ':' and a command name followed by a space separated list of
arguments.

	> :load test.json

The above loads the file "test.json" into the jqsh cache for inspection.  There
is no quoting of arguments.  A plus '+' may be used on the last argument to
include all charactors up to (but excluding) the next newline character.

	> :push +.items[] | select(.name | contains("hello"))

The above pushes the filter `.items[] | select(.name | contains("hello"))` on
to the jqsh filter stack. This is such a common operation that it has a special
shorthand.  A non-empty line that does not start with a colon causes the line's
contents to be pushed on the filter stack. So the above line could be
simplified.

	> .[] | select(.gender == "Female")

Blank lines are also a shorthand, printing the working filter stack applied to
the input, equivalent to the "write" command.

	> :write

Command reference

A list of commands and other interactive help topics can be found with through
"help" command.

	> :help

Individual commands respond to the "-h" flag for usage documentation.
*/
package documentation

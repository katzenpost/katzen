The purpose of this empty module is to allow us to use a pinned fork of gogio
with this as-yet unmerged PR: https://github.com/gioui/gio/pull/67

...since "go install" will only install a fork if you search+replace to change
the module name everywhere, or have a replace directive in a go.mod file.
(An issue which is tracked here: https://github.com/golang/go/issues/50278 )

Previously we did this in katzen's go.mod file, but go mod tidy would always
remove all of the indirect dependencies. So, now we have this empty module for
the replace directive to live in intsead. (Do not run go mod tidy here.)

When gio#67 is merged, this should go away.

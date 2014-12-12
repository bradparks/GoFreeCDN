This program will examine files in a directory structure and generate a Go google app engine
instance to serve static data files as URLs. These static data files will be split into chunks
and they will be compressed with gzip.

INSTALL:
	go build gofreecdn.go
	cp gofreecdn ~/bin

USAGE:
	gofreecdn -dir DIR -appdir DIR -appname STR

This example usage will read static files from the current directory and recurse
into subdirectories while writing the google app engine files to ../../ServeFileApp
You need to create the GAE instance and replace sinuous-vortex-111 with the name
of your GAE instance.

	gofreecdn -appname sinuous-vortex-111 -appdir ../../ServeFileApp

It is also possible to read static files from a named directory while writing
GAE instance files to the current directory like so:

	gofreecdn -appname sinuous-vortex-111 -dir ../StaticFilesDir

You must have installed the most recent app engine SDK for go:

https://cloud.google.com/appengine/docs/go/

RUN LOCALLY:
	goapp serve

DEPLOY TO GAE:
	goapp deploy -oauth


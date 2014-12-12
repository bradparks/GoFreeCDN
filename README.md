Go implementation of a "Free CDN service" hosted on google app engine. GAE provides 1 gig of
bandwidth per day so a quick and dirty CDN works well as long as files are not too large.
One can always enable billing to support a production environment later on, but this
implementation makes it easy to try out a free CDN service without having to enable billing.

https://appengine.google.com/

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

Test the generated GAE instance by running the following commands in the
directory where the GAE instance files were written. This is the --appdir
directory of the current dir if using the -dir argument.

RUN LOCALLY:

	goapp serve

DEPLOY TO GAE:

	goapp deploy -oauth

All data files are returned as a JSON buffer that contains gzip encoded chunks
of files. This makes it possible to reduce bandwidth for files that can be
compressed easily. In addition, this implementation works around GAE limitation
of 32 megs as a max file size. The JSON buffer also contains exact file sizes
for each chunk so that a client can display accurate file download progress.

Example localhost curl:

	curl -v http://localhost:8080/File.dat
{
    "Luna_480p.mp4" =     (
                {
            ChunkName = "http://localhost:8080/chunk/C1285965654409315471649.gz";
            CompressedLength = 29894585;
        },
                {
            ChunkName = "http://localhost:8080/chunk/C1264214449450425662994.gz";
            CompressedLength = 30002313;
        },
                {
            ChunkName = "http://localhost:8080/chunk/C1247170876055113801956.gz";
            CompressedLength = 11790335;
        }
    );
}


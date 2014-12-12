// gofreecdn
//
// This program will examine files in a directory structure and generate a Go google app engine
// instance to serve the files via a URL.
//
// gofreecdn -dir DIR -appdir DIR -appname STR
//
// -dir     : input directory that contains the input files which is scanned recursively
// -appdir  : output directory where GAE deployable project is written
// -appname : the name of the GAE service, like "sinuous-vortex-700" without the appspot.com suffix.

package main

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var appDirStr *string

// Map file filename like "Foo.m4v" to array of chunk names
// and the download size for each chunk

type ChunkEntry struct {
	ChunkName        string `json:"ChunkName"`
	CompressedLength int    `json:"CompressedLength"`
}

var chunkMap map[string][]ChunkEntry = make(map[string][]ChunkEntry)

var chunkUID int32 = 0

// GAE max file size is 32 megs

const maxFileSize int = 32000000

// Fully qualified paths for chunk dir

var chunkDirPath string

// This util method generates a very large random number as a string
// It is really really unlikely that two chunks would come out with
// the same filename using this approach.

func randStr() string {
	var f float64 = rand.Float64() * 10000000000
	var s string = fmt.Sprintf("%.10f", f)
	s = strings.Replace(s, ".", "", -1)
	return s
}

// Open and query the file size

func filesize(filepath string) (int, error) {
	fd, err := os.Open(filepath)
	if err != nil {
		return -1, err
	}

	defer fd.Close()

	fileInfo, err := fd.Stat()
	if err != nil {
		return -1, err
	}

	return int(fileInfo.Size()), nil
}

// This util method will write a buffer to a file with gzip
// compression applied.

func writeGzipFile(filepath string, buffer []byte, level int) error {
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	outgz, err := gzip.NewWriterLevel(out, level)

	if err != nil {
		return err
	}
	defer outgz.Close()

	var ntowrite int64 = int64(len(buffer))
	var written int64

	bReader := bytes.NewReader(buffer)

	written, err = io.CopyN(outgz, bReader, ntowrite)

	if err != nil {
		return err
	}

	if written != ntowrite {
		return errors.New("Copy chunk did not copy all bytes")
	}

	return nil
}

// This method will break a large file up into 32 meg chunks and then gzip each chunk
// so that it can be served up as a standalone file. It is critical to gzip the chunk
// after breaking it up so that each chunk can be decoded and streamed into a file
// by the embedded client. Otherwise, the client would need to read all the file, write
// a large gz file, and then decode that and this would result in a lot more io in
// the time critical load path.

func copyFileChunks(src, dstDir string, numBytes int) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}

	defer in.Close()

	var maxFileSizePadded int = maxFileSize - 2000000

	var numChunks int = int(numBytes / maxFileSizePadded)
	var rem int = int(numBytes % maxFileSizePadded)
	if rem != 0 {
		numChunks += 1
	}

	var chunks []ChunkEntry = make([]ChunkEntry, numChunks)

	timeDate := time.Now()

	for i := 0; i < numChunks; i++ {
		var err error

		timeStrPrefix := fmt.Sprintf("%02d", timeDate.Month())

		var chunkName string = fmt.Sprintf("C%s%s.gz", timeStrPrefix, randStr())

		var chunkPath string = fmt.Sprintf("%s/%s", chunkDirPath, chunkName)

		fmt.Printf("%s : chunk %d = %s\n", src, i, chunkPath)

		chunkUID += 1

		// Copy bytes in chunk into a []byte

		var copyNBytes int64 = int64(maxFileSizePadded)
		if i == numChunks-1 {
			copyNBytes = int64(rem)
		}

		byteArr := make([]byte, copyNBytes)
		numBytesRead, err := io.ReadAtLeast(in, byteArr, int(copyNBytes))

		if numBytesRead != int(copyNBytes) {
			return errors.New("ReadFrom() did not copy all bytes")
		}

		// Write with "gzip -9" first

		err = writeGzipFile(chunkPath, byteArr, gzip.BestCompression)

		if err != nil {
			return err
		}

		compressedNBytes, err := filesize(chunkPath)
		if err != nil {
			return err
		}

		fmt.Printf("%s : gzip -9 numbytes = %d\n", src, compressedNBytes)
		fmt.Printf("%s : orig    numbytes = %d\n", src, int(copyNBytes))

		// If the compressed size got larger than the original then use no compression

		if compressedNBytes >= int(copyNBytes) {
			err = writeGzipFile(chunkPath, byteArr, gzip.NoCompression)

			if err != nil {
				return err
			}

			compressedZeroNBytes, err := filesize(chunkPath)

			if err != nil {
				return err
			}

			fmt.Printf("%s : gzip -0 numbytes = %d\n", src, compressedZeroNBytes)

			compressedNBytes = compressedZeroNBytes
		}

		if err != nil {
			return err
		}

		entry := ChunkEntry{}
		entry.ChunkName = chunkName
		entry.CompressedLength = compressedNBytes
		chunks[i] = entry
	}

	chunkMap[src] = chunks

	return nil
}

func visitValidFile(path string, fileInfo os.FileInfo) {
	fmt.Printf("Visited Valid: %s\n", path)

	// If the size of the file is larger that 32megs then split

	var numBytes int64 = fileInfo.Size()

	err := copyFileChunks(path, *appDirStr, int(numBytes))
	if err != nil {
		fmt.Printf("error in copy chunks for path %s\n", path)
		os.Exit(1)
	}
}

func visit(path string, fileInfo os.FileInfo, err error) error {
	var debugVisit bool = true

	if debugVisit {
		fmt.Printf("Visited: %s\n", path)
	}
	if IsDirectory(path) {
		// Ignore unless the directory starts with a "." like ".git"
		// but treat "." and ".." as normal directory names

		if path != "." && path != ".." {
			if len(path) > 1 && path[0] == '.' {
				if debugVisit {
					fmt.Printf("Skip dot prefixed dir: %s\n", path)
				}
				return filepath.SkipDir
			}
		}
	} else {
		if len(path) > 1 && path[0] == '.' {
			// Ignore hidden files
			if debugVisit {
				fmt.Printf("Skip hidden file: %s\n", path)
			}
		} else {
			if fileInfo.Mode().IsRegular() == false {
				fmt.Printf("Skip non-regular file: %s\n", path)
			} else {
				visitValidFile(path, fileInfo)
			}
		}
	}

	return nil
}

func IsDirectory(dir string) bool {
	src, err := os.Stat(dir)

	if err != nil && os.IsNotExist(err) {
		return false
	}

	if err != nil {
		panic(err)
	}

	if src.IsDir() {
		return true
	} else {
		return false
	}
}

func VerifyDirectory(dir string, argname string) {
	if !IsDirectory(dir) {
		fmt.Printf("%s is not a valid directory\n", argname)
		os.Exit(1)
	}
}

// Format and emit the GAE app.yaml file. This configuration file indicates
// how static files are mapped to URLs served by the app.

func format_app_yaml(appName string, appDir string) error {
	var err error
	var buffer bytes.Buffer

	buffer.WriteString(fmt.Sprintf("application: %s\n", appName))
	buffer.WriteString("version: 1\n")
	buffer.WriteString("runtime: go\n")
	buffer.WriteString("api_version: go1\n")
	buffer.WriteString("\n")

	if false {
		// Cache for 60 seconds in "dev" mode so that changes can be seen soonish
		buffer.WriteString("default_expiration: \"1m\"\n")
		buffer.WriteString("\n")
	} else {
		buffer.WriteString("default_expiration: \"30d\"\n")
		buffer.WriteString("\n")
	}

	buffer.WriteString("handlers:\n")
	buffer.WriteString("\n")

	// Every file is split into chunks (even if only 1 chunk)
	// and then the file info is delivered via a JSON buffer.

	for chunkFilename, _ := range chunkMap {
		buffer.WriteString(fmt.Sprintf("- url: /%s\n", chunkFilename))
		buffer.WriteString("  script: _go_app\n")
		buffer.WriteString("\n")
	}

	// Every chunk file is served as a static file

	buffer.WriteString("- url: /chunk/*\n")
	buffer.WriteString("  static_dir: chunk\n")
	buffer.WriteString("\n")

	// Write to "app.yaml"

	yamlPath := fmt.Sprintf("%s/app.yaml", appDir)
	err = ioutil.WriteFile(yamlPath, buffer.Bytes(), 0644)
	if err != nil {
		return err
	}

	return nil
}

// Format and emit chunk configuration file as JSON named "chunks.json.gz",
// this file stores the mapping between large static filenames
// and the file chunks that hold the file data. This file contains
// the name of the chunk files along with the download size, since
// the http protocol cannot be relied upon to deliver the Content-Length
// header that indicates the total size of the download.

func format_chunk_json(appName string, appDir string) error {
	bytes, err := json.MarshalIndent(chunkMap, "", "  ")
	if err != nil {
		return err
	}

	bytes = append(bytes, '\n')

	// Write to "big.json.gz", this reduces install size in the
	// case where the JSON is really large.

	jsonGZPath := fmt.Sprintf("%s/chunks.json.gz", appDir)

	err = writeGzipFile(jsonGZPath, bytes, gzip.BestCompression)
	if err != nil {
		return err
	}

	return nil
}

// This util method is invoked to write go source code that will be compiled
// and uploaded to implement the server side handler.

func write_server_go(appDir string) error {
	var err error

	goSrcPath := fmt.Sprintf("%s/%s", appDir, "servefile.go")

	fmt.Printf("writing %s\n", goSrcPath)

	outFile, err := os.Create(goSrcPath)

	if err != nil {
		return err
	}

	defer outFile.Close()

	// Contents of servefile.go base64 encoded:
	// base64 -i servefile.go
	// go build gofreecdn.go
	// cp gofreecdn ~/bin

	var encoded string = "cGFja2FnZSBzZXJ2ZWZpbGUKCmltcG9ydCAoCgkiYXBwZW5naW5lIgoJLy8iZW5jb2RpbmcvYmFzZTY0IgoJImNvbXByZXNzL2d6aXAiCgkiZW5jb2RpbmcvanNvbiIKCSJmbXQiCgkibG9nIgoJIm5ldC9odHRwIgoJIm9zIgoJLy8iaW8iCgkiaW8vaW91dGlsIgoJInN0cmluZ3MiCikKCmZ1bmMgaW5pdCgpIHsKCWh0dHAuSGFuZGxlRnVuYygiLyIsIGhhbmRsZXIpCn0KCi8vIFRoZSBKc29uIGlucHV0IGZpbGUgImJpZy5qc29uIiB3aWxsIGNvbnRhaW4gMCAtPiBOIGluc3RhbmNlcyBvZiB0aGUKLy8gZm9sbG93aW5nIGRhdGF0eXBlIHVzZWQgdG8gcmVjb25zdHJ1Y3QgYSBsYXJnZXIgZmlsZSBmcm9tIGEgbnVtYmVyCi8vIG9mIDMyIG1lZyBjaHVua3MgKHRoZSBtYXggR0FFIHdpbGwgdXBsb2FkIGZvciBvbmUgZmlsZSkuCgp0eXBlIENodW5rRW50cnkgc3RydWN0IHsKCUNodW5rTmFtZSAgICAgICAgc3RyaW5nIGBqc29uOiJDaHVua05hbWUiYAoJQ29tcHJlc3NlZExlbmd0aCBpbnQgICAgYGpzb246IkNvbXByZXNzZWRMZW5ndGgiYAp9Cgp2YXIgY2h1bmtNYXAgbWFwW3N0cmluZ11bXUNodW5rRW50cnkgPSBtYWtlKG1hcFtzdHJpbmddW11DaHVua0VudHJ5KQoKdmFyIGNodW5rTWFwUGFyc2VkIGJvb2wgPSBmYWxzZQoKZnVuYyByZWFkR3pGaWxlKGZpbGVuYW1lIHN0cmluZykgKFtdYnl0ZSwgZXJyb3IpIHsKCWZpLCBlcnIgOj0gb3MuT3BlbihmaWxlbmFtZSkKCWlmIGVyciAhPSBuaWwgewoJCXJldHVybiBuaWwsIGVycgoJfQoJZGVmZXIgZmkuQ2xvc2UoKQoKCXJlYWRlciwgZXJyIDo9IGd6aXAuTmV3UmVhZGVyKGZpKQoJaWYgZXJyICE9IG5pbCB7CgkJcmV0dXJuIG5pbCwgZXJyCgl9CglkZWZlciByZWFkZXIuQ2xvc2UoKQoKCWJ5dGVzLCBlcnIgOj0gaW91dGlsLlJlYWRBbGwocmVhZGVyKQoJaWYgZXJyICE9IG5pbCB7CgkJcmV0dXJuIG5pbCwgZXJyCgl9CglyZXR1cm4gYnl0ZXMsIG5pbAp9CgpmdW5jIHBhcnNlX2NodW5rX21hcChjIGFwcGVuZ2luZS5Db250ZXh0KSBlcnJvciB7Cgl2YXIgZXJyIGVycm9yCgoJaWYgY2h1bmtNYXBQYXJzZWQgPT0gdHJ1ZSB7CgkJcmV0dXJuIG5pbAoJfQoKCWJ5dGVzLCBlcnIgOj0gcmVhZEd6RmlsZSgiY2h1bmtzLmpzb24uZ3oiKQoJaWYgZXJyICE9IG5pbCB7CgkJcmV0dXJuIGVycgoJfQoKCS8vbG9nLkZhdGFsKCJyZWFkIGJ5dGVzOiIsIHN0cmluZyhieXRlcykpCgoJZXJyID0ganNvbi5Vbm1hcnNoYWwoYnl0ZXMsICZjaHVua01hcCkKCgljLkluZm9mKCJQYXJzZWQgJWQgYnl0ZXMgb2YgSlNPTiBpbnRvICVkIG1hcCBlbnRyaWVzIiwgbGVuKGJ5dGVzKSwgbGVuKGNodW5rTWFwKSkKCglpZiBlcnIgIT0gbmlsIHsKCQlyZXR1cm4gZXJyCgl9CgoJY2h1bmtNYXBQYXJzZWQgPSB0cnVlCgoJcmV0dXJuIG5pbAp9CgovLyBFdmVyeSBmaWxlIGlzIHJlcHJlc2VudGVkIGJ5IGEgSlNPTiBwYXlsb2FkIHdpdGggbWV0YWRhdGEgYWJvdXQgdGhlCi8vIGNodW5rcyB0aGF0IGNvbnRhaW4gdGhlIGZpbGUgZGF0YS4gVGhlIEdBRSB1cGxvYWRlciBoYXMgYSBoYXJkIGxpbWl0Ci8vIG9mIDMyIG1lZ3MgZm9yIGVhY2ggc3RhdGljIGZpbGUsIHNvIGNodW5rcyBhcmUgcmVxdWlyZWQgdG8gc3VwcG9ydAovLyBmaWxlcyBsYXJnZXIgdGhhbiAzMiBtZWdzLiBFYWNoIGNodW5rIGlzIGd6aXAgY29tcHJlc3NlZCBzbyBldmVuIGZpbGVzCi8vIHRoYXQgYXJlIHNtYWxsIGNvdWxkIGdldCBzbWFsbGVyIHdoZW4gdHVybmVkIGludG8gY2h1bmtzLiBUaGlzIGFsc28KLy8gbWFrZXMgdGhlIGNsaWVudCBjb2RlIGNsZWFuZXIgc2luY2UgdGhlIGNsaWVudCB3aWxsIG9ubHkgZXZlciBoYW5kCi8vIGd6aXAgY29tcHJlc3NlZCBkYXRhLgoKZnVuYyBoYW5kbGVyKHcgaHR0cC5SZXNwb25zZVdyaXRlciwgciAqaHR0cC5SZXF1ZXN0KSB7Cgljb250ZXh0IDo9IGFwcGVuZ2luZS5OZXdDb250ZXh0KHIpCgoJZXJyIDo9IHBhcnNlX2NodW5rX21hcChjb250ZXh0KQoKCWlmIGVyciAhPSBuaWwgewoJCWxvZy5GYXRhbCgiZXJyb3I6IiwgZXJyKQoJfSBlbHNlIHsKCQkvLyBEZXRlcm1pbmUgd2hpY2ggZmlsZSBpcyBiZWluZyByZXF1ZXN0ZWQgdGhlbiBjb25zdHJ1Y3QgY2FjaGVkIHZlcnNpb24KCQkvLyBieSBjb2xsZWN0aW5nIHRoZSBjaHVua3MgdG9nZXRoZXIgaW50byBvbmUgYmlnIGRvd25sb2FkLgoKCQl3LkhlYWRlcigpLlNldCgiQ29udGVudC1UeXBlIiwgImFwcGxpY2F0aW9uL2pzb24iKQoKCQl2YXIgY2h1bmtNYXBXaXRoVXJscyBtYXBbc3RyaW5nXVtdQ2h1bmtFbnRyeSA9IG1ha2UobWFwW3N0cmluZ11bXUNodW5rRW50cnkpCgoJCWJpZ0ZpbGVuYW1lIDo9IHIuVVJMLlBhdGgKCgkJaWYgbGVuKGJpZ0ZpbGVuYW1lKSA9PSAwIHsKCQkJbG9nLkZhdGFsKGZtdC5TcHJpbnRmKCJwYXRoIFwiJXNcIiIsIGJpZ0ZpbGVuYW1lKSkKCQl9IGVsc2UgaWYgYmlnRmlsZW5hbWVbMF0gIT0gJy8nIHsKCQkJbG9nLkZhdGFsKGZtdC5TcHJpbnRmKCJwYXRoIFwiJXNcIiIsIGJpZ0ZpbGVuYW1lKSkKCQl9IGVsc2UgaWYgc3RyaW5ncy5Db3VudChiaWdGaWxlbmFtZSwgIi8iKSAhPSAxIHsKCQkJbG9nLkZhdGFsKGZtdC5TcHJpbnRmKCJwYXRoIFwiJXNcIiIsIGJpZ0ZpbGVuYW1lKSkKCQl9CgoJCWJpZ0ZpbGVuYW1lID0gYmlnRmlsZW5hbWVbMTpdCgkJY2h1bmtBcnIgOj0gY2h1bmtNYXBbYmlnRmlsZW5hbWVdCgoJCXsKCQkJdmFyIGNodW5rcyBbXUNodW5rRW50cnkgPSBtYWtlKFtdQ2h1bmtFbnRyeSwgbGVuKGNodW5rQXJyKSkKCgkJCWZvciBpLCBjaHVua0VudHJ5IDo9IHJhbmdlIGNodW5rQXJyIHsKCQkJCWNodW5rc1tpXSA9IGNodW5rRW50cnkKCQkJCWNodW5rc1tpXS5DaHVua05hbWUgPSBmbXQuU3ByaW50ZigiJXMlcy9jaHVuay8lcyIsICJodHRwOi8vIiwgYXBwZW5naW5lLkRlZmF1bHRWZXJzaW9uSG9zdG5hbWUoY29udGV4dCksIGNodW5rRW50cnkuQ2h1bmtOYW1lKQoJCQl9CgoJCQljaHVua01hcFdpdGhVcmxzW2JpZ0ZpbGVuYW1lXSA9IGNodW5rcwoJCX0KCgkJYnl0ZXMsIGVyciA6PSBqc29uLk1hcnNoYWxJbmRlbnQoY2h1bmtNYXBXaXRoVXJscywgIiIsICIgICIpCgkJaWYgZXJyICE9IG5pbCB7CgkJCWxvZy5GYXRhbCgiZXJyb3I6IiwgZXJyKQoJCX0KCgkJYnl0ZXMgPSBhcHBlbmQoYnl0ZXMsICdcbicpCgoJCV8sIGVyciA9IHcuV3JpdGUoYnl0ZXMpCgkJaWYgZXJyICE9IG5pbCB7CgkJCWxvZy5GYXRhbCgiZXJyb3I6IiwgZXJyKQoJCX0KCX0KfQo="

	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded))

	_, err = io.Copy(outFile, reader)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	var err error

	rand.Seed(time.Now().Unix())

	dirStr := flag.String("dir", ".", "input dir path")
	appDirStr = flag.String("appdir", ".", "output app dir path")
	appNameStr := flag.String("appname", "", "GAE app name")
	flag.Parse()

	if *appNameStr == "" {
		fmt.Printf("%s must be passed\n", "-appname")
		os.Exit(1)
	}

	VerifyDirectory(*dirStr, "-dir")
	VerifyDirectory(*appDirStr, "-appdir")

	// If both -dir and -appdir are both "." then one of the two is missing

	if *dirStr == "." && *appDirStr == "." {
		fmt.Printf("one of the -dir or -appdir must be passed\n")
		os.Exit(1)
	}

	// Both -dir and -appdir cannot be the same path

	if *dirStr == *appDirStr {
		fmt.Printf("bot -dir and -appdir cannot be the same path\n")
		os.Exit(1)
	}

	// -dir cannot be "/", same goes for -appdir

	if *dirStr == "/" || *appDirStr == "/" {
		fmt.Printf("-dir or -appdir must not be the root directory\n")
		os.Exit(1)
	}

	fmt.Printf("Reading files from %s\n", *dirStr)
	fmt.Printf("Writing to app dir %s\n", *appDirStr)

	chunkDirPath = fmt.Sprintf("%s/%s", *appDirStr, "chunk")

	var paths = [...]string{chunkDirPath}

	for _, dirPath := range paths {
		//fmt.Printf("checking for dir %s\n", dirPath)
		//fmt.Printf("IsDirectory(%s) = %t\n", dirPath, IsDirectory(dirPath))

		if IsDirectory(dirPath) {
			err = os.RemoveAll(dirPath)

			if err != nil {
				fmt.Printf("%v\n", err)
				os.Exit(1)
			}
		}

		if true {
			err = os.Mkdir(dirPath, 0700)

			if err != nil {
				fmt.Printf("%v\n", err)
				os.Exit(1)
			}

			fmt.Printf("mkdir %s\n", dirPath)
		}
	}

	err = filepath.Walk(*dirStr, visit)

	if err != nil {
		fmt.Printf("filepath.Walk() returned %v\n", err)
		os.Exit(1)
	}

	for key, chunkEntryArr := range chunkMap {
		fmt.Printf("Filename %s\n", key)

		for _, chunkEntry := range chunkEntryArr {
			fmt.Printf("\t%28s numbytes %d\n", chunkEntry.ChunkName, chunkEntry.CompressedLength)
		}
	}

	err = format_app_yaml(*appNameStr, *appDirStr)
	if err != nil {
		fmt.Printf("format_app_yaml err %v\n", err)
		os.Exit(1)
	}

	err = format_chunk_json(*appNameStr, *appDirStr)
	if err != nil {
		fmt.Printf("format_chunk_json err %v\n", err)
		os.Exit(1)
	}

	err = write_server_go(*appDirStr)
	if err != nil {
		fmt.Printf("write_server_go err %v\n", err)
		os.Exit(1)
	}

}

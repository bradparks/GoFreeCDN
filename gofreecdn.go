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

func copyFileContents(src, dst string) (err error) {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return nil
	}
	defer func() {
		cerr := out.Close()
		if err == nil {
			err = cerr
		}
	}()
	if _, err = io.Copy(out, in); err != nil {
		return nil
	}
	err = out.Sync()
	return err
}

func copyFile(src, dst string) (err error) {
	err = os.Link(src, dst)
	if err != nil {
		return copyFileContents(src, dst)
	} else {
		return nil
	}
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

// Format and emit chunk configuration file as JSON named "chunks.json",
// this file stores the mapping between large static filenames
// and the file chunks that hold the file data. This file contains
// the name of the chunk files along with the download size, since
// the http protocol cannot be relied upon to deliver the Content-Length
// header that indicates the total size of the download.

// FIXME: gzip encode the JSON data to reduce deploy size

func format_chunk_json(appName string, appDir string) error {
	bytes, err := json.MarshalIndent(chunkMap, "", "  ")
	if err != nil {
		return err
	}

	bytes = append(bytes, '\n')

	// Write to "big.json"

	yamlPath := fmt.Sprintf("%s/chunks.json", appDir)
	err = ioutil.WriteFile(yamlPath, bytes, 0644)
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

	var encoded string = "cGFja2FnZSBzZXJ2ZWZpbGUKCmltcG9ydCAoCgkiYXBwZW5naW5lIgoJLy8iZW5jb2RpbmcvYmFzZTY0IgoJImVuY29kaW5nL2pzb24iCgkiZm10IgoJImxvZyIKCSJuZXQvaHR0cCIKCS8vImlvIgoJImlvL2lvdXRpbCIKCSJzdHJpbmdzIgopCgpmdW5jIGluaXQoKSB7CglodHRwLkhhbmRsZUZ1bmMoIi8iLCBoYW5kbGVyKQp9CgovLyBUaGUgSnNvbiBpbnB1dCBmaWxlICJiaWcuanNvbiIgd2lsbCBjb250YWluIDAgLT4gTiBpbnN0YW5jZXMgb2YgdGhlCi8vIGZvbGxvd2luZyBkYXRhdHlwZSB1c2VkIHRvIHJlY29uc3RydWN0IGEgbGFyZ2VyIGZpbGUgZnJvbSBhIG51bWJlcgovLyBvZiAzMiBtZWcgY2h1bmtzICh0aGUgbWF4IEdBRSB3aWxsIHVwbG9hZCBmb3Igb25lIGZpbGUpLgoKdHlwZSBDaHVua0VudHJ5IHN0cnVjdCB7CglDaHVua05hbWUgICAgICAgIHN0cmluZyBganNvbjoiQ2h1bmtOYW1lImAKCUNvbXByZXNzZWRMZW5ndGggaW50ICAgIGBqc29uOiJDb21wcmVzc2VkTGVuZ3RoImAKfQoKdmFyIGNodW5rTWFwIG1hcFtzdHJpbmddW11DaHVua0VudHJ5ID0gbWFrZShtYXBbc3RyaW5nXVtdQ2h1bmtFbnRyeSkKCnZhciBjaHVua01hcFBhcnNlZCBib29sID0gZmFsc2UKCmZ1bmMgcGFyc2VfY2h1bmtfbWFwKGMgYXBwZW5naW5lLkNvbnRleHQpIGVycm9yIHsKCXZhciBlcnIgZXJyb3IKCglpZiBjaHVua01hcFBhcnNlZCA9PSB0cnVlIHsKCQlyZXR1cm4gbmlsCgl9CgoJYnl0ZXMsIGVyciA6PSBpb3V0aWwuUmVhZEZpbGUoImNodW5rcy5qc29uIikKCWlmIGVyciAhPSBuaWwgewoJCXJldHVybiBlcnIKCX0KCgkvL2xvZy5GYXRhbCgicmVhZCBieXRlczoiLCBzdHJpbmcoYnl0ZXMpKQoKCWVyciA9IGpzb24uVW5tYXJzaGFsKGJ5dGVzLCAmY2h1bmtNYXApCgoJYy5JbmZvZigiUGFyc2VkICVkIGJ5dGVzIG9mIEpTT04gaW50byAlZCBtYXAgZW50cmllcyIsIGxlbihieXRlcyksIGxlbihjaHVua01hcCkpCgoJaWYgZXJyICE9IG5pbCB7CgkJcmV0dXJuIGVycgoJfQoKCWNodW5rTWFwUGFyc2VkID0gdHJ1ZQoKCXJldHVybiBuaWwKfQoKLy8gRXZlcnkgZmlsZSBpcyByZXByZXNlbnRlZCBieSBhIEpTT04gcGF5bG9hZCB3aXRoIG1ldGFkYXRhIGFib3V0IHRoZQovLyBjaHVua3MgdGhhdCBjb250YWluIHRoZSBmaWxlIGRhdGEuIFRoZSBHQUUgdXBsb2FkZXIgaGFzIGEgaGFyZCBsaW1pdAovLyBvZiAzMiBtZWdzIGZvciBlYWNoIHN0YXRpYyBmaWxlLCBzbyBjaHVua3MgYXJlIHJlcXVpcmVkIHRvIHN1cHBvcnQKLy8gZmlsZXMgbGFyZ2VyIHRoYW4gMzIgbWVncy4gRWFjaCBjaHVuayBpcyBnemlwIGNvbXByZXNzZWQgc28gZXZlbiBmaWxlcwovLyB0aGF0IGFyZSBzbWFsbCBjb3VsZCBnZXQgc21hbGxlciB3aGVuIHR1cm5lZCBpbnRvIGNodW5rcy4gVGhpcyBhbHNvCi8vIG1ha2VzIHRoZSBjbGllbnQgY29kZSBjbGVhbmVyIHNpbmNlIHRoZSBjbGllbnQgd2lsbCBvbmx5IGV2ZXIgaGFuZAovLyBnemlwIGNvbXByZXNzZWQgZGF0YS4KCmZ1bmMgaGFuZGxlcih3IGh0dHAuUmVzcG9uc2VXcml0ZXIsIHIgKmh0dHAuUmVxdWVzdCkgewoJY29udGV4dCA6PSBhcHBlbmdpbmUuTmV3Q29udGV4dChyKQoKCWVyciA6PSBwYXJzZV9jaHVua19tYXAoY29udGV4dCkKCglpZiBlcnIgIT0gbmlsIHsKCQlsb2cuRmF0YWwoImVycm9yOiIsIGVycikKCX0gZWxzZSB7CgkJLy8gRGV0ZXJtaW5lIHdoaWNoIGZpbGUgaXMgYmVpbmcgcmVxdWVzdGVkIHRoZW4gY29uc3RydWN0IGNhY2hlZCB2ZXJzaW9uCgkJLy8gYnkgY29sbGVjdGluZyB0aGUgY2h1bmtzIHRvZ2V0aGVyIGludG8gb25lIGJpZyBkb3dubG9hZC4KCgkJdy5IZWFkZXIoKS5TZXQoIkNvbnRlbnQtVHlwZSIsICJhcHBsaWNhdGlvbi9qc29uIikKCgkJdmFyIGNodW5rTWFwV2l0aFVybHMgbWFwW3N0cmluZ11bXUNodW5rRW50cnkgPSBtYWtlKG1hcFtzdHJpbmddW11DaHVua0VudHJ5KQoKCQliaWdGaWxlbmFtZSA6PSByLlVSTC5QYXRoCgoJCWlmIGxlbihiaWdGaWxlbmFtZSkgPT0gMCB7CgkJCWxvZy5GYXRhbChmbXQuU3ByaW50ZigicGF0aCBcIiVzXCIiLCBiaWdGaWxlbmFtZSkpCgkJfSBlbHNlIGlmIGJpZ0ZpbGVuYW1lWzBdICE9ICcvJyB7CgkJCWxvZy5GYXRhbChmbXQuU3ByaW50ZigicGF0aCBcIiVzXCIiLCBiaWdGaWxlbmFtZSkpCgkJfSBlbHNlIGlmIHN0cmluZ3MuQ291bnQoYmlnRmlsZW5hbWUsICIvIikgIT0gMSB7CgkJCWxvZy5GYXRhbChmbXQuU3ByaW50ZigicGF0aCBcIiVzXCIiLCBiaWdGaWxlbmFtZSkpCgkJfQoKCQliaWdGaWxlbmFtZSA9IGJpZ0ZpbGVuYW1lWzE6XQoJCWNodW5rQXJyIDo9IGNodW5rTWFwW2JpZ0ZpbGVuYW1lXQoKCQl7CgkJCXZhciBjaHVua3MgW11DaHVua0VudHJ5ID0gbWFrZShbXUNodW5rRW50cnksIGxlbihjaHVua0FycikpCgoJCQlmb3IgaSwgY2h1bmtFbnRyeSA6PSByYW5nZSBjaHVua0FyciB7CgkJCQljaHVua3NbaV0gPSBjaHVua0VudHJ5CgkJCQljaHVua3NbaV0uQ2h1bmtOYW1lID0gZm10LlNwcmludGYoIiVzJXMvY2h1bmsvJXMiLCAiaHR0cDovLyIsIGFwcGVuZ2luZS5EZWZhdWx0VmVyc2lvbkhvc3RuYW1lKGNvbnRleHQpLCBjaHVua0VudHJ5LkNodW5rTmFtZSkKCQkJfQoKCQkJY2h1bmtNYXBXaXRoVXJsc1tiaWdGaWxlbmFtZV0gPSBjaHVua3MKCQl9CgoJCWJ5dGVzLCBlcnIgOj0ganNvbi5NYXJzaGFsSW5kZW50KGNodW5rTWFwV2l0aFVybHMsICIiLCAiICAiKQoJCWlmIGVyciAhPSBuaWwgewoJCQlsb2cuRmF0YWwoImVycm9yOiIsIGVycikKCQl9CgoJCWJ5dGVzID0gYXBwZW5kKGJ5dGVzLCAnXG4nKQoKCQlfLCBlcnIgPSB3LldyaXRlKGJ5dGVzKQoJCWlmIGVyciAhPSBuaWwgewoJCQlsb2cuRmF0YWwoImVycm9yOiIsIGVycikKCQl9Cgl9Cn0K"

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

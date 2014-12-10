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
	"os"
	"path/filepath"
	"strings"
)

var appDirStr *string

// Map file filename like "Foo.m4v" to array of chunk names

var chunkMap map[string][]string = make(map[string][]string)

var chunkUID int32 = 0

// GAE max file size is 32 megs

const maxFileSize int = 32000000

// Fully qualified paths for static files dir and chunk files dir

var staticDirPath string
var chunkDirPath string

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

	var numChunks int = int(numBytes / maxFileSize)
	var rem int = int(numBytes % maxFileSize)
	if rem != 0 {
		numChunks += 1
	}

	var chunks []string = make([]string, numChunks)

	for i := 0; i < numChunks; i++ {
		var err error

		var chunkName string = fmt.Sprintf("Chunk%d.gz", chunkUID+1)

		var chunkPath string = fmt.Sprintf("%s/%s", chunkDirPath, chunkName)

		fmt.Printf("%s : chunk %d = %s\n", src, i, chunkPath)

		chunkUID += 1

		out, err := os.Create(chunkPath)
		if err != nil {
			return err
		}

		defer out.Close()

		var copyNBytes int64 = int64(maxFileSize)
		if i == numChunks-1 {
			copyNBytes = int64(rem)
		}

		outgz, err := gzip.NewWriterLevel(out, 9)

		if err != nil {
			return err
		}

		defer outgz.Close()

		var written int64
		written, err = io.CopyN(outgz, in, copyNBytes)

		if err != nil {
			return err
		}

		if written != copyNBytes {
			return errors.New("Copy chunk did not copy all bytes")
		}

		chunks[i] = chunkName
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

	const forceChunkingOfAllFiles bool = false

	if numBytes > int64(maxFileSize) || forceChunkingOfAllFiles {
		err := copyFileChunks(path, *appDirStr, int(numBytes))
		if err != nil {
			fmt.Printf("error in copy chunks for path %s\n", path)
			os.Exit(1)
		}
	} else {
		staticPath := fmt.Sprintf("%s/%s", staticDirPath, path)

		if true {
			fmt.Printf("cp %s %s\n", path, staticPath)
		}

		err := copyFile(path, staticPath)
		if err != nil {
			fmt.Printf("error in copy for path %s to %s\n", path, staticPath)
			os.Exit(1)
		}
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

	// Each big file must be listed as an exception to the static
	// rules so that the URL request is delivered to the go script.

	for chunkFilename, _ := range chunkMap {
		buffer.WriteString(fmt.Sprintf("- url: /%s\n", chunkFilename))
		buffer.WriteString("  script: _go_app\n")
		buffer.WriteString("\n")
	}

	// Every chunk file is smaller than the max request size
	// and each can be served as a static chunk.

	// Every small file is treated as a static URL which should
	// execute more quickly than a call that executes go code.

	buffer.WriteString("- url: /chunk/*\n")
	buffer.WriteString("  static_dir: chunk\n")
	buffer.WriteString("\n")

	// Every small file is treated as a static URL which should
	// execute more quickly than a call that executes go code.

	buffer.WriteString("- url: /*\n")
	buffer.WriteString("  static_dir: static\n")
	buffer.WriteString("\n")

	// Write to "app.yaml"

	yamlPath := fmt.Sprintf("%s/app.yaml", appDir)
	err = ioutil.WriteFile(yamlPath, buffer.Bytes(), 0644)
	if err != nil {
		return err
	}

	return nil
}

// Format and emit chunk configuration file as JSON named "big.json",
// this file stores the mapping between large static filenames
// and the file chunks that hold the file data.

// FIXME: gzip encode the JSON data to reduce deploy size

func format_chunk_json(appName string, appDir string) error {
	bytes, err := json.MarshalIndent(chunkMap, "", "  ")
	if err != nil {
		return err
	}

	bytes = append(bytes, '\n')

	// Write to "big.json"

	yamlPath := fmt.Sprintf("%s/big.json", appDir)
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

	var encoded string = "cGFja2FnZSBzZXJ2ZWZpbGUKCmltcG9ydCAoCgkiYXBwZW5naW5lIgoJLy8iZW5jb2RpbmcvYmFzZTY0IgoJImVuY29kaW5nL2pzb24iCgkiZm10IgoJImxvZyIKCSJuZXQvaHR0cCIKCS8vImlvIgoJImlvL2lvdXRpbCIKCSJzdHJpbmdzIgopCgpmdW5jIGluaXQoKSB7CglodHRwLkhhbmRsZUZ1bmMoIi8iLCBoYW5kbGVyKQp9CgovLyBUaGUgSnNvbiBpbnB1dCBmaWxlICJiaWcuanNvbiIgd2lsbCBjb250YWluIDAgLT4gTiBpbnN0YW5jZXMgb2YgdGhlCi8vIGZvbGxvd2luZyBkYXRhdHlwZSB1c2VkIHRvIHJlY29uc3RydWN0IGEgbGFyZ2VyIGZpbGUgZnJvbSBhIG51bWJlcgovLyBvZiAzMiBtZWcgY2h1bmtzICh0aGUgbWF4IEdBRSB3aWxsIHVwbG9hZCBmb3Igb25lIGZpbGUpLgoKdmFyIGNodW5rTWFwIG1hcFtzdHJpbmddW11zdHJpbmcgPSBtYWtlKG1hcFtzdHJpbmddW11zdHJpbmcpCgp2YXIgY2h1bmtNYXBQYXJzZWQgYm9vbCA9IGZhbHNlCgpmdW5jIHBhcnNlX2NodW5rX21hcChjIGFwcGVuZ2luZS5Db250ZXh0KSBlcnJvciB7Cgl2YXIgZXJyIGVycm9yCgoJaWYgY2h1bmtNYXBQYXJzZWQgPT0gdHJ1ZSB7CgkJcmV0dXJuIG5pbAoJfQoKCWJ5dGVzLCBlcnIgOj0gaW91dGlsLlJlYWRGaWxlKCJiaWcuanNvbiIpCglpZiBlcnIgIT0gbmlsIHsKCQlyZXR1cm4gZXJyCgl9CgoJLy9sb2cuRmF0YWwoInJlYWQgYnl0ZXM6Iiwgc3RyaW5nKGJ5dGVzKSkKCgllcnIgPSBqc29uLlVubWFyc2hhbChieXRlcywgJmNodW5rTWFwKQoKCWMuSW5mb2YoIlBhcnNlZCAlZCBieXRlcyBvZiBKU09OIGludG8gJWQgbWFwIGVudHJpZXMiLCBsZW4oYnl0ZXMpLCBsZW4oY2h1bmtNYXApKQoKCWlmIGVyciAhPSBuaWwgewoJCXJldHVybiBlcnIKCX0KCgljaHVua01hcFBhcnNlZCA9IHRydWUKCglyZXR1cm4gbmlsCn0KCi8vIEEgbGFyZ2UgZmlsZSBpcyBoYW5kbGVkIGJ5IGNyZWF0aW5nIGEgSlNPTiBwYXlsb2FkIHRoYXQgY29udGFpbnMgdGhlCi8vIG5hbWUgb2YgdGhlIHJldHVybmVkIGZpbGUgYW5kIHRoZSBsaXN0IG9mIHN0YXRpYyBjaHVua3MgdGhhdCBtYWtlIHVwCi8vIHRoZSBmaWxlLiBUaGUgY2xpZW50IG11c3QgbWFrZSByZXF1ZXN0cyBmb3IgZWFjaCBjaHVuayBvbmUgYnkgb25lCi8vIHNpbmNlIHRoZSBHQUUgaW5zdGFuY2UgaGFzIGEgaGFyZCBsaW1pdCBvZiBhYm91dCAzMiBtZWdzIGZvciBvbmUKLy8gcmVxdWVzdC4gVGhpcyBpbXBsZW1lbnRhdGlvbiBhY3R1YWxseSByZWR1Y2VzIGxvYWQgb24gdGhlIEdBRSBpbnN0YW5jZQovLyBzaW5jZSB0aGVyZSBpcyBubyBuZWVkIHRvIHN0cmVhbSB0aGUgZGF0YSBhbmQgdGhlIGNhY2hlIGNhbiBob2xkIHRoZQovLyBzbWFsbGVyIGNodW5rcyB3aGljaCBhcmUgdGhlbiBhc3NlbWJsZWQgYnkgdGhlIGNsaWVudC4KCmZ1bmMgaGFuZGxlcih3IGh0dHAuUmVzcG9uc2VXcml0ZXIsIHIgKmh0dHAuUmVxdWVzdCkgewoJY29udGV4dCA6PSBhcHBlbmdpbmUuTmV3Q29udGV4dChyKQoKCWVyciA6PSBwYXJzZV9jaHVua19tYXAoY29udGV4dCkKCglpZiBlcnIgIT0gbmlsIHsKCQlsb2cuRmF0YWwoImVycm9yOiIsIGVycikKCX0gZWxzZSB7CgkJLy8gRGV0ZXJtaW5lIHdoaWNoIGZpbGUgaXMgYmVpbmcgcmVxdWVzdGVkIHRoZW4gY29uc3RydWN0IGNhY2hlZCB2ZXJzaW9uCgkJLy8gYnkgY29sbGVjdGluZyB0aGUgY2h1bmtzIHRvZ2V0aGVyIGludG8gb25lIGJpZyBkb3dubG9hZC4KCgkJdy5IZWFkZXIoKS5TZXQoIkNvbnRlbnQtVHlwZSIsICJhcHBsaWNhdGlvbi9qc29uIikKCgkJdmFyIGNodW5rTWFwV2l0aFVybHMgbWFwW3N0cmluZ11bXXN0cmluZyA9IG1ha2UobWFwW3N0cmluZ11bXXN0cmluZykKCgkJYmlnRmlsZW5hbWUgOj0gci5VUkwuUGF0aAoKCQlpZiBsZW4oYmlnRmlsZW5hbWUpID09IDAgewoJCQlsb2cuRmF0YWwoZm10LlNwcmludGYoInBhdGggXCIlc1wiIiwgYmlnRmlsZW5hbWUpKQoJCX0gZWxzZSBpZiBiaWdGaWxlbmFtZVswXSAhPSAnLycgewoJCQlsb2cuRmF0YWwoZm10LlNwcmludGYoInBhdGggXCIlc1wiIiwgYmlnRmlsZW5hbWUpKQoJCX0gZWxzZSBpZiBzdHJpbmdzLkNvdW50KGJpZ0ZpbGVuYW1lLCAiLyIpICE9IDEgewoJCQlsb2cuRmF0YWwoZm10LlNwcmludGYoInBhdGggXCIlc1wiIiwgYmlnRmlsZW5hbWUpKQoJCX0KCgkJYmlnRmlsZW5hbWUgPSBiaWdGaWxlbmFtZVsxOl0KCQljaHVua0FyciA6PSBjaHVua01hcFtiaWdGaWxlbmFtZV0KCgkJewoJCQl2YXIgY2h1bmtzIFtdc3RyaW5nID0gbWFrZShbXXN0cmluZywgbGVuKGNodW5rQXJyKSkKCgkJCWZvciBpLCBjaHVua0ZpbGVuYW1lIDo9IHJhbmdlIGNodW5rQXJyIHsKCQkJCWNodW5rc1tpXSA9IGZtdC5TcHJpbnRmKCIlcy9jaHVuay8lcyIsIGFwcGVuZ2luZS5EZWZhdWx0VmVyc2lvbkhvc3RuYW1lKGNvbnRleHQpLCBjaHVua0ZpbGVuYW1lKQoJCQl9CgoJCQljaHVua01hcFdpdGhVcmxzW2JpZ0ZpbGVuYW1lXSA9IGNodW5rcwoJCX0KCgkJYnl0ZXMsIGVyciA6PSBqc29uLk1hcnNoYWxJbmRlbnQoY2h1bmtNYXBXaXRoVXJscywgIiIsICIgICIpCgkJaWYgZXJyICE9IG5pbCB7CgkJCWxvZy5GYXRhbCgiZXJyb3I6IiwgZXJyKQoJCX0KCgkJYnl0ZXMgPSBhcHBlbmQoYnl0ZXMsICdcbicpCgoJCV8sIGVyciA9IHcuV3JpdGUoYnl0ZXMpCgkJaWYgZXJyICE9IG5pbCB7CgkJCWxvZy5GYXRhbCgiZXJyb3I6IiwgZXJyKQoJCX0KCX0KfQo="

	reader := base64.NewDecoder(base64.StdEncoding, strings.NewReader(encoded))

	_, err = io.Copy(outFile, reader)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	var err error

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

	staticDirPath = fmt.Sprintf("%s/%s", *appDirStr, "static")
	chunkDirPath = fmt.Sprintf("%s/%s", *appDirStr, "chunk")

	var paths = [...]string{staticDirPath, chunkDirPath}

	for _, dirPath := range paths {
		//fmt.Printf("checking for dir %s\n", dirPath)
		//fmt.Printf("IsDirectory(%s) = %t\n", dirPath, IsDirectory(dirPath))

		if IsDirectory(dirPath) == false {
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

	for key, strArr := range chunkMap {
		fmt.Printf("Filename %s\n", key)

		for _, chunkName := range strArr {
			fmt.Printf("\t%s\n", chunkName)
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

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
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

var appDirStr *string

// Map file filename like "Foo.m4v" to array of chunk names

var chunkMap map[string][]string = make(map[string][]string)

var chunkUID int32 = 0

// GAE max file size is 32 megs

const maxFileSize int = 32000000

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
		var chunkName string = fmt.Sprintf("Chunk%d", chunkUID+1)

		var chunkPath string = fmt.Sprintf("%s/%s", dstDir, chunkName)

		fmt.Printf("%s : chunk %d = %s\n", src, i, chunkPath)

		chunkUID += 1

		out, err := os.Create(chunkPath)
		if err != nil {
			return err
		}

		var copyNBytes int64 = int64(maxFileSize)
		if i == numChunks-1 {
			copyNBytes = int64(rem)
		}

		var written int64
		written, err = io.CopyN(out, in, copyNBytes)

		if err != nil {
			return err
		}

		if written != copyNBytes {
			return errors.New("Copy chunk did not copy all bytes")
		}

		out.Close()

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

	if numBytes > int64(maxFileSize) {
		err := copyFileChunks(path, *appDirStr, int(numBytes))
		if err != nil {
			fmt.Printf("error in copy chunks for path %s", path)
			os.Exit(1)
		}
	} else {
		dst := fmt.Sprintf("%s/%s", *appDirStr, path)

		if true {
			fmt.Printf("cp %s %s\n", path, dst)
		}

		err := copyFile(path, dst)
		if err != nil {
			fmt.Printf("error in copy for path %s to %s", path, dst)
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
		fmt.Printf("%s is not a valid directory : ", argname)
		os.Exit(1)
	}
}

func main() {
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

	err := filepath.Walk(*dirStr, visit)

	if err != nil {
		fmt.Printf("filepath.Walk() returned %v\n", err)
	}

	for key, strArr := range chunkMap {
		fmt.Printf("Filename %s\n", key)

		for _, chunkName := range strArr {
			fmt.Printf("\t%s\n", chunkName)
		}
	}

}

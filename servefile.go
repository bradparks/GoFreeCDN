package servefile

import (
	"appengine"
	//"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	//"io"
	"io/ioutil"
	"strings"
)

func init() {
	http.HandleFunc("/", handler)
}

// The Json input file "big.json" will contain 0 -> N instances of the
// following datatype used to reconstruct a larger file from a number
// of 32 meg chunks (the max GAE will upload for one file).

type ChunkEntry struct {
	ChunkName        string `json:"ChunkName"`
	CompressedLength int    `json:"CompressedLength"`
}

var chunkMap map[string][]ChunkEntry = make(map[string][]ChunkEntry)

var chunkMapParsed bool = false

func parse_chunk_map(c appengine.Context) error {
	var err error

	if chunkMapParsed == true {
		return nil
	}

	bytes, err := ioutil.ReadFile("chunks.json")
	if err != nil {
		return err
	}

	//log.Fatal("read bytes:", string(bytes))

	err = json.Unmarshal(bytes, &chunkMap)

	c.Infof("Parsed %d bytes of JSON into %d map entries", len(bytes), len(chunkMap))

	if err != nil {
		return err
	}

	chunkMapParsed = true

	return nil
}

// Every file is represented by a JSON payload with metadata about the
// chunks that contain the file data. The GAE uploader has a hard limit
// of 32 megs for each static file, so chunks are required to support
// files larger than 32 megs. Each chunk is gzip compressed so even files
// that are small could get smaller when turned into chunks. This also
// makes the client code cleaner since the client will only ever hand
// gzip compressed data.

func handler(w http.ResponseWriter, r *http.Request) {
	context := appengine.NewContext(r)

	err := parse_chunk_map(context)

	if err != nil {
		log.Fatal("error:", err)
	} else {
		// Determine which file is being requested then construct cached version
		// by collecting the chunks together into one big download.

		w.Header().Set("Content-Type", "application/json")

		var chunkMapWithUrls map[string][]ChunkEntry = make(map[string][]ChunkEntry)

		bigFilename := r.URL.Path

		if len(bigFilename) == 0 {
			log.Fatal(fmt.Sprintf("path \"%s\"", bigFilename))
		} else if bigFilename[0] != '/' {
			log.Fatal(fmt.Sprintf("path \"%s\"", bigFilename))
		} else if strings.Count(bigFilename, "/") != 1 {
			log.Fatal(fmt.Sprintf("path \"%s\"", bigFilename))
		}

		bigFilename = bigFilename[1:]
		chunkArr := chunkMap[bigFilename]

		{
			var chunks []ChunkEntry = make([]ChunkEntry, len(chunkArr))

			for i, chunkEntry := range chunkArr {
				chunks[i] = chunkEntry
				chunks[i].ChunkName = fmt.Sprintf("%s%s/chunk/%s", "http://", appengine.DefaultVersionHostname(context), chunkEntry.ChunkName)
			}

			chunkMapWithUrls[bigFilename] = chunks
		}

		bytes, err := json.MarshalIndent(chunkMapWithUrls, "", "  ")
		if err != nil {
			log.Fatal("error:", err)
		}

		bytes = append(bytes, '\n')

		_, err = w.Write(bytes)
		if err != nil {
			log.Fatal("error:", err)
		}
	}
}

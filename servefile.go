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
)

func init() {
	http.HandleFunc("/", handler)
}

// The Json input file "big.json" will contain 0 -> N instances of the
// following datatype used to reconstruct a larger file from a number
// of 32 meg chunks (the max GAE will upload for one file).

var chunkMap map[string][]string = make(map[string][]string)

var chunkMapParsed bool = false

func parse_chunk_map(c appengine.Context) error {
	var err error

	if chunkMapParsed == true {
		return nil
	}

	bytes, err := ioutil.ReadFile("big.json")
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

// A large file is handled by creating a JSON payload that contains the
// name of the returned file and the list of static chunks that make up
// the file. The client must make requests for each chunk one by one
// since the GAE instance has a hard limit of about 32 megs for one
// request. This implementation actually reduces load on the GAE instance
// since there is no need to stream the data and the cache can hold the
// smaller chunks which are then assembled by the client.

func handler(w http.ResponseWriter, r *http.Request) {
	context := appengine.NewContext(r)

	err := parse_chunk_map(context)

	if err != nil {
		log.Fatal("error:", err)
	} else {
		// Determine which file is being requested then construct cached version
		// by collecting the chunks together into one big download.

		w.Header().Set("Content-Type", "application/json")

		var chunkMapWithUrls map[string][]string = make(map[string][]string)

		for bigFilename, chunkArr := range chunkMap {
			var chunks []string = make([]string, len(chunkArr))

			for i, chunkFilename := range chunkArr {
				chunks[i] = fmt.Sprintf("%s/chunk/%s", appengine.DefaultVersionHostname(context), chunkFilename)
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

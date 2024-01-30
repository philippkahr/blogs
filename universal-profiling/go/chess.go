package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var CloudID string = ""
var ElasticPassword string = os.Getenv("ELASTIC_PASSWORD")
var ElasticUsername string = "elastic"
var re1 = regexp.MustCompile(`\{.*?\}`)
var re2 = regexp.MustCompile(`(\d+\.{3})`)
var re3 = regexp.MustCompile(`\?!|!\?|\?*|!*`)
var re4 = regexp.MustCompile(`\s+`)
var re5 = regexp.MustCompile(` (1\/2-1\/2|\d-\d)`)

func calculateSHA256Hash(data string) string {
	hash := sha256.New()
	hash.Write([]byte(data))
	hashInBytes := hash.Sum(nil)
	hashString := hex.EncodeToString(hashInBytes)
	return hashString
}

func readHeader(incoming <-chan []string, toBulk chan<- []Doc) {
	// games should be 10000 games as an array
	for games := range incoming {
		docs := make([]Doc, 0, len(games))
		today := time.Now().Format("2006-01-02T15:04:05.000Z")
		for _, game := range games {
			// Process each game
			header := make(map[string]string)
			lines := strings.Split(strings.TrimSpace(game), "\n")
			for _, line := range lines {
				if line == "" {
					// Empty line
					continue
				} else if line[0] == '[' {
					// Parsing header lines
					line = strings.TrimPrefix(line, "[")
					line = strings.TrimSuffix(line, "]")
					parts := strings.Split(line, " \"")
					if len(parts) == 2 {
						key := strings.TrimSpace(parts[0])
						value := strings.TrimSuffix(strings.TrimSpace(parts[1]), "\"")
						header[key] = value
					}
				} else if line[0] == '1' && line[1] == '.' {
					// Processing moves line
					header["moves"] = strings.TrimSpace(line)
				}
			}
			var timestamp string
			if _, ok := header["UTCDate"]; ok {
				if _, ok := header["UTCTime"]; ok {
					timestamp = fmt.Sprintf("%sT%sZ", header["UTCDate"], header["UTCTime"])
					timestamp = strings.Replace(timestamp, ".", "-", -1)
					_, err := time.Parse(time.RFC3339, timestamp)
					if err != nil {
						fmt.Println("Error parsing time:", err)
						// let's place it in the future
						timestamp = "2030-01-01T12:00:00.000+00:00"
					}
				}
			}

			doc := Doc{
				Index:   "chess-summaries",
				Op_type: "create",
				Source: Source{
					Timestamp:   timestamp,
					Db:          "lichess",
					Event:       Event{Ingested: today},
					Name:        header["Event"],
					Game_id:     calculateSHA256Hash(game),
					Url:         header["Site"],
					Data_stream: Datastream{Namespace: "default", Type: "summary", Dataset: "chess-games"},
					Moves:       Moves{Original: header["moves"]},
					User: User{
						White: UserDetails{Name: header["White"], Elo: eloToInt(header["WhiteElo"]), Diff: eloToInt(header["WhiteRatingDiff"])},
						Black: UserDetails{Name: header["Black"], Elo: eloToInt(header["BlackElo"]), Diff: eloToInt(header["BlackRatingDiff"])},
					},
					Opening:     Opening{Eco: header["ECO"], Name: header["Opening"]},
					Termination: strings.ToLower(header["Termination"]),
					Timecontrol: header["TimeControl"],
				},
			}
			results := handleResult(header["Result"])
			doc.Source.Result = Result{
				Outcome: header["Result"],
				White:   results["white"],
				Black:   results["black"],
				Draw:    results["draw"],
			}
			if doc.Source.Moves.Original != "" && (strings.Contains(doc.Source.Moves.Original, "...") || strings.Contains(doc.Source.Moves.Original, "{")) {
				clean := re1.ReplaceAllString(doc.Source.Moves.Original, "")
				clean = re2.ReplaceAllString(clean, "")
				clean = re3.ReplaceAllString(clean, "")
				clean = re4.ReplaceAllString(clean, " ")
				clean = re5.ReplaceAllString(clean, "")
				doc.Source.Moves.Cleaned = clean
			}

			docs = append(docs, doc)
		}
		log.Printf("Sending %d docs to bulk indexer", len(docs))
		toBulk <- docs[:len(docs)]
	}
}

func handleResult(result string) map[string]bool {
	// Handle the result
	var resultMap map[string]bool
	if result == "1-0" {
		resultMap = map[string]bool{"white": true}
	} else if result == "0-1" {
		resultMap = map[string]bool{"black": true}
	} else if result == "1/2-1/2" {
		resultMap = map[string]bool{"draw": true}
	}
	return resultMap
}

func eloToInt(elo string) int {
	// Convert elo to int
	if elo == "?" || elo == "" {
		return 0
	}
	eloInt, err := strconv.Atoi(elo)
	if err != nil {
		fmt.Println("Error converting elo to int:", err)
		return 0
	}
	return eloInt
}

func readGames(filePath string) (processed int, err error) {
	file, err := os.Open(filePath)
	if err != nil {
		return processed, fmt.Errorf("error opening file %s: %v", filePath, err)
	}
	defer file.Close()

	toProcess := make(chan []string)
	toBulkIndexer := make(chan []Doc)
	defer close(toProcess)
	defer close(toBulkIndexer)

	// Background parallel processing of games, outputting to bulkIndexer
	for i := 0; i < runtime.NumCPU(); i++ {
		go readHeader(toProcess, toBulkIndexer)
	}
	// Background ingestion process into Elasticsearch through BulkIndexer
	bulkIndexer := bulkIndexerClient("chess-summaries", 10<<10)
	ctx, stopIngestion := context.WithCancel(context.Background())
	go indexToElastic(ctx, toBulkIndexer, bulkIndexer)

	// number of batched items for processing
	const batchSize = 10000
	// total number of games processed
	gamesCounter := 0

	games := make([]string, 0, batchSize)
	tempStr := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var line string = scanner.Text()
		if strings.TrimSpace(line) == "" {
			// empty line
			tempStr += line + "\n"
		} else if line[0] == '1' && line[1] == '.' {
			//game is done
			tempStr += line + "\n"
			games = append(games, tempStr)
			tempStr = ""
		} else if line[0] == '[' {
			// all the lines with the information
			tempStr += line + "\n"
		}
		if len(games) > 0 && len(games)%batchSize == 0 {
			toProcess <- games[:len(games)]
			log.Printf("Processing %d more games", len(games))
			gamesCounter += len(games)
			games = make([]string, 0, batchSize)
		}
	}
	toProcess <- games[:len(games)]
	gamesCounter += len(games)
	log.Printf("Completed processing %d games", gamesCounter)
	stopIngestion()

	return processed, scanner.Err()
}

func main() {
	file := flag.String("file", "", "Path to PGN file")
	flag.Parse()

	games, err := readGames(*file)
	if err != nil {
		log.Fatalf("Error reading games: %s", err)
	}
	log.Printf("Processed %d games", games)
}

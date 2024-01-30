package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"runtime"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esutil"
)

func bulkIndexerClient(indexName string, flushBytes uint) esutil.BulkIndexer {
	es, err := elasticsearch.NewClient(elasticsearch.Config{
		CloudID:  CloudID,
		Username: ElasticUsername,
		Password: ElasticPassword,
	})
	if err != nil {
		log.Fatalf("Error creating the Elasticsearch client: %v", err)
	}
	bi, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Index:      indexName,        // The default index name
		Client:     es,               // The Elasticsearch client
		NumWorkers: runtime.NumCPU(), // The number of worker goroutines
		FlushBytes: int(flushBytes),  // The flush threshold in bytes
		OnError: func(ctx context.Context, err error) {
			log.Printf("Bulkindexer error: %v", err)
		},
	})
	if err != nil {
		log.Fatalf("Error creating the indexer: %s", err)
	}
	return bi
}

func indexToElastic(ctx context.Context, ch <-chan []Doc, bi esutil.BulkIndexer) {
	for docs := range ch {
		for _, doc := range docs {
			data, err := json.Marshal(doc.Source)
			if err != nil {
				log.Fatalf("Cannot encode article: %s", err)
			}
			if err := bi.Add(ctx, esutil.BulkIndexerItem{
				Index:     doc.Index,
				Action:    doc.Op_type,
				Body:      bytes.NewReader(data),
				OnSuccess: nil,
			}); err != nil {
				log.Println("Error adding document to bulk:", err)
			}
		}
		log.Printf("Done adding to the bulk indexer %d", len(docs))
	}
	err := bi.Close(context.Background())
	if err != nil {
		log.Printf("Error closing the bulk indexer: %s", err)
	}
}

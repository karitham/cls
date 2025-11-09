package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	chroma "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/amikos-tech/chroma-go/pkg/embeddings"
	ollama "github.com/amikos-tech/chroma-go/pkg/embeddings/ollama"
	"golang.org/x/sync/errgroup"
)

type FileData struct {
	Path    string
	Name    string
	Content string
	Size    int64
}
type FileMetadata struct {
	Filename string
	Path     string
	Size     int64
}
type QueryResult struct {
	FileName string
	Path     string
	Content  string
}
type ChromaClient interface {
	GetOrCreateCollection(ctx context.Context, name string) (Collection, error)
	GetCollection(ctx context.Context, name string) (Collection, error)
	DeleteCollection(ctx context.Context, name string) error
	Close() error
}
type Collection interface {
	AddDocuments(ctx context.Context, paths []string) error
	Query(ctx context.Context, query string, n int) ([]QueryResult, error)
}
type chromaClientImpl struct {
	client chroma.Client
	ef     embeddings.EmbeddingFunction
	logger *slog.Logger
}

func NewChromaClient(chromaURL string, logger *slog.Logger) (ChromaClient, error) {
	client, err := chroma.NewHTTPClient(chroma.WithBaseURL(chromaURL))
	if err != nil {
		return nil, fmt.Errorf("failed to create ChromaDB client: %w", err)
	}

	ef, efErr := ollama.NewOllamaEmbeddingFunction(
		ollama.WithBaseURL("http://127.0.0.1:11434"),
		ollama.WithModel("nomic-embed-text"),
	)
	if efErr != nil {
		client.Close()
		return nil, fmt.Errorf("error creating Ollama embedding function: %w", efErr)
	}

	return &chromaClientImpl{
		client: client,
		ef:     ef,
		logger: logger,
	}, nil
}

func (c *chromaClientImpl) GetOrCreateCollection(ctx context.Context, name string) (Collection, error) {
	coll, err := c.client.GetOrCreateCollection(ctx, name, chroma.WithEmbeddingFunctionCreate(c.ef))
	if err != nil {
		return nil, fmt.Errorf("failed to get/create collection: %w", err)
	}
	return &collectionImpl{coll: coll, logger: c.logger}, nil
}

func (c *chromaClientImpl) GetCollection(ctx context.Context, name string) (Collection, error) {
	coll, err := c.client.GetCollection(ctx, name, chroma.WithEmbeddingFunctionGet(c.ef))
	if err != nil {
		return nil, fmt.Errorf("failed to get collection: %w", err)
	}
	return &collectionImpl{coll: coll, logger: c.logger}, nil
}

func (c *chromaClientImpl) DeleteCollection(ctx context.Context, name string) error {
	err := c.client.DeleteCollection(ctx, name)
	if err != nil {
		return fmt.Errorf("failed to delete collection: %w", err)
	}
	return nil
}

func (c *chromaClientImpl) Close() error {
	return c.client.Close()
}

type collectionImpl struct {
	coll   chroma.Collection
	logger *slog.Logger
}

func (c *collectionImpl) AddDocuments(ctx context.Context, paths []string) error {
	return BatchAddDocuments(ctx, c.coll, paths, c.logger)
}

func (c *collectionImpl) Query(ctx context.Context, query string, n int) ([]QueryResult, error) {
	results, err := c.coll.Query(ctx,
		chroma.WithQueryTexts(query),
		chroma.WithIncludeQuery(chroma.IncludeDocuments, chroma.IncludeMetadatas),
		chroma.WithNResults(n),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query collection: %w", err)
	}

	documents := results.GetDocumentsGroups()
	metadatas := results.GetMetadatasGroups()

	if len(documents) == 0 || len(documents[0]) == 0 {
		return []QueryResult{}, nil
	}

	var queryResults []QueryResult
	for i, doc := range documents[0] {
		result := QueryResult{
			Content: fmt.Sprintf("%v", doc),
		}
		if len(metadatas) > 0 && i < len(metadatas[0]) {
			metadata := metadatas[0][i]
			if filename, ok := metadata.GetString("filename"); ok {
				result.FileName = filename
			}
			if path, ok := metadata.GetString("path"); ok {
				result.Path = path
			}
		}
		queryResults = append(queryResults, result)
	}

	return queryResults, nil
}
func BatchAddDocuments(ctx context.Context, coll chroma.Collection, paths []string, logger *slog.Logger) error {
	if len(paths) == 0 {
		return nil
	}

	group, _ := errgroup.WithContext(ctx)
	group.SetLimit(50)

	batchSize := 100
	for i := 0; i < len(paths); i += batchSize {
		paths := paths[i:max(i+batchSize, len(paths))]

		group.Go(func() error {
			var (
				docsMeta    = make([]chroma.DocumentMetadata, len(paths))
				docIDs      = make([]chroma.DocumentID, len(paths))
				docContents = make([]string, len(paths))
			)
			for i, p := range paths {
				data, err := os.ReadFile(p)
				if err != nil {
					continue
				}

				docsMeta[i] = chroma.NewDocumentMetadata(chroma.NewStringAttribute("path", string(p)))
				docIDs[i] = chroma.DocumentID(p)
				docContents[i] = string(data)
			}

			err := coll.Add(ctx,
				chroma.WithIDs(docIDs...),
				chroma.WithTexts(docContents...),
				chroma.WithMetadatas(docsMeta...))
			if err != nil {
				return fmt.Errorf("failed to add documents to collection: %w", err)
			}

			return nil
		})
	}

	return group.Wait()
}

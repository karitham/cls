package main

import (
	"context"
	"fmt"
	"log/slog"

	chroma "github.com/amikos-tech/chroma-go/pkg/api/v2"
	"github.com/amikos-tech/chroma-go/pkg/embeddings"
	ollama "github.com/amikos-tech/chroma-go/pkg/embeddings/ollama"
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
	AddDocuments(ctx context.Context, ids []string, documents []string, metadatas []FileMetadata) error
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

func (c *collectionImpl) AddDocuments(ctx context.Context, ids []string, documents []string, metadatas []FileMetadata) error {
	return BatchAddDocuments(ctx, c.coll, ids, documents, metadatas, c.logger)
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
func BatchAddDocuments(ctx context.Context, coll chroma.Collection, ids []string, documents []string, metadatas []FileMetadata, logger *slog.Logger) error {
	if len(ids) != len(documents) || len(ids) != len(metadatas) {
		return fmt.Errorf("ids, documents, and metadatas must have the same length")
	}

	if len(ids) == 0 {
		return nil
	}
	documentIDs := make([]chroma.DocumentID, len(ids))
	for i, id := range ids {
		documentIDs[i] = chroma.DocumentID(id)
	}

	chromaMetadatas := make([]chroma.DocumentMetadata, len(metadatas))
	for i, meta := range metadatas {
		chromaMetadatas[i] = chroma.NewDocumentMetadata(
			chroma.NewStringAttribute("filename", meta.Filename),
			chroma.NewStringAttribute("path", meta.Path),
			chroma.NewIntAttribute("size", meta.Size),
		)
	}
	batchSize := 100
	for i := 0; i < len(documentIDs); i += batchSize {
		end := i + batchSize
		if end > len(documentIDs) {
			end = len(documentIDs)
		}

		err := coll.Add(ctx,
			chroma.WithIDs(documentIDs[i:end]...),
			chroma.WithTexts(documents[i:end]...),
			chroma.WithMetadatas(chromaMetadatas[i:end]...))
		if err != nil {
			return fmt.Errorf("failed to add documents batch %d-%d to collection: %w", i, end-1, err)
		}
	}

	return nil
}

package store

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
)

// Similar is a nearest-neighbor result.
type Similar struct {
	ID    string
	Score float64 // cosine similarity, in [-1, 1]
}

// SetEmbedding stores a node's embedding vector as a float32 blob.
func (s *Store) SetEmbedding(id string, vec []float32) error {
	res, err := s.db.Exec(`UPDATE nodes SET embedding = ? WHERE id = ?`,
		encodeFloats(vec), id)
	if err != nil {
		return fmt.Errorf("store: set embedding: %w", err)
	}
	return requireAffected(res, id)
}

// GetEmbedding returns a node's embedding vector.
func (s *Store) GetEmbedding(id string) ([]float32, error) {
	var blob []byte
	err := s.db.QueryRow(`SELECT embedding FROM nodes WHERE id = ?`, id).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get embedding: %w", err)
	}
	if blob == nil {
		return nil, ErrNotFound
	}
	return decodeFloats(blob), nil
}

// HasEmbedding reports whether a node has an embedding stored.
func (s *Store) HasEmbedding(id string) bool {
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM nodes WHERE id = ? AND embedding IS NOT NULL`, id).Scan(&one)
	return err == nil
}

// NearestNeighbors returns the k nodes whose embeddings are most similar to
// the given node's, excluding the node itself. Cosine similarity is computed
// in memory over all embedded nodes; fine for personal-scale corpora.
func (s *Store) NearestNeighbors(id string, k int) ([]Similar, error) {
	if k <= 0 {
		return nil, nil
	}
	query, err := s.GetEmbedding(id)
	if err != nil {
		return nil, err
	}

	rows, err := s.db.Query(`SELECT id, embedding FROM nodes WHERE embedding IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("store: neighbors: %w", err)
	}
	defer rows.Close()

	var results []Similar
	for rows.Next() {
		var (
			otherID string
			blob    []byte
		)
		if err := rows.Scan(&otherID, &blob); err != nil {
			return nil, fmt.Errorf("store: neighbors: %w", err)
		}
		if otherID == id {
			continue
		}
		vec := decodeFloats(blob)
		if len(vec) != len(query) {
			continue // dimension mismatch: skip
		}
		results = append(results, Similar{ID: otherID, Score: cosine(query, vec)})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > k {
		results = results[:k]
	}
	return results, nil
}

func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func encodeFloats(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(f))
	}
	return b
}

func decodeFloats(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

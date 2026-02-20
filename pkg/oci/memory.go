package oci

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"

	"github.com/opencontainers/go-digest"
)

var _ Client = &Memory{}

type AtomicLock struct {
	state int32 // 0: unlocked, 1: locked
}

type Memory struct {
	blobs  map[digest.Digest][]byte
	tags   map[string]digest.Digest
	images []Image
	lock   AtomicLock
}

func (l *AtomicLock) Lock() {
	for !atomic.CompareAndSwapInt32(&l.state, 0, 1) {
		// If the state is 0 (unlocked), try to change it to 1 (locked)
		// Otherwise, keep spinning until it succeeds (busy-waiting)
	}
}

func (l *AtomicLock) Unlock() {
	// Set the state back to 0 (unlocked)
	atomic.StoreInt32(&l.state, 0)
}

func NewMemory() *Memory {
	return &Memory{
		images: []Image{},
		tags:   map[string]digest.Digest{},
		blobs:  map[digest.Digest][]byte{},
	}
}

func (m *Memory) Name() string {
	return "memory"
}

func (m *Memory) Verify(ctx context.Context) error {
	return nil
}

func (m *Memory) Subscribe(ctx context.Context) (<-chan ImageEvent, <-chan error, error) {
	return nil, nil, nil
}

func (m *Memory) ListImages(ctx context.Context) ([]Image, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	return m.images, nil
}

func (m *Memory) Resolve(ctx context.Context, ref string) (digest.Digest, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	dgst, ok := m.tags[ref]
	if !ok {
		return "", fmt.Errorf("could not resolve tag %s to a digest", ref)
	}
	return dgst, nil
}

func (m *Memory) Size(ctx context.Context, dgst digest.Digest) (int64, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	b, ok := m.blobs[dgst]
	if !ok {
		return 0, errors.Join(ErrNotFound, fmt.Errorf("size information for digest %s not found", dgst))
	}
	return int64(len(b)), nil
}

func (m *Memory) GetManifest(ctx context.Context, dgst digest.Digest) ([]byte, string, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	b, ok := m.blobs[dgst]
	if !ok {
		return nil, "", errors.Join(ErrNotFound, fmt.Errorf("manifest with digest %s not found", dgst))
	}
	mt, err := DetermineMediaType(b)
	if err != nil {
		return nil, "", err
	}
	return b, mt, nil
}

func (m *Memory) GetBlob(ctx context.Context, dgst digest.Digest) (io.ReadSeekCloser, error) {
	m.lock.Lock()
	defer m.lock.Unlock()

	b, ok := m.blobs[dgst]
	if !ok {
		return nil, errors.Join(ErrNotFound, fmt.Errorf("blob with digest %s not found", dgst))
	}
	rc := io.NewSectionReader(bytes.NewReader(b), 0, int64(len(b)))
	return struct {
		io.ReadSeeker
		io.Closer
	}{
		ReadSeeker: rc,
		Closer:     io.NopCloser(nil),
	}, nil
}

func (m *Memory) AddImage(img Image) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.images = append(m.images, img)
	tagName, ok := img.TagName()
	if !ok {
		return
	}
	m.tags[tagName] = img.Digest
}

func (m *Memory) AddBlob(b []byte, dgst digest.Digest) {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.blobs[dgst] = b
}

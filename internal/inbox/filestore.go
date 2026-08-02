package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"sync"

	"github.com/tus/tusd/v2/pkg/filestore"
	"github.com/tus/tusd/v2/pkg/handler"
)

type uploadStore struct {
	disk filestore.FileStore
	in   *Inbox
}

func (s *uploadStore) NewUpload(ctx context.Context, info handler.FileInfo) (handler.Upload, error) {
	created, err := s.disk.NewUpload(ctx, info)
	if err != nil {
		return nil, err
	}
	stored, err := created.GetInfo(ctx)
	if err != nil {
		return nil, err
	}
	if !s.in.register(stored) {
		return created, nil
	}
	return &liveUpload{in: s.in, info: stored}, nil
}

func (s *uploadStore) GetUpload(ctx context.Context, id string) (handler.Upload, error) {
	info, isLive, err := s.in.liveInfo(id)
	if err != nil {
		return nil, err
	}
	if !isLive {
		return s.disk.GetUpload(ctx, id)
	}
	return &liveUpload{in: s.in, info: info}, nil
}

func (s *uploadStore) AsTerminatableUpload(upload handler.Upload) handler.TerminatableUpload {
	if live, ok := upload.(*liveUpload); ok {
		return live
	}
	return s.disk.AsTerminatableUpload(upload)
}

func (s *uploadStore) AsLengthDeclarableUpload(upload handler.Upload) handler.LengthDeclarableUpload {
	if live, ok := upload.(*liveUpload); ok {
		return live
	}
	return s.disk.AsLengthDeclarableUpload(upload)
}

type liveUpload struct {
	in   *Inbox
	info handler.FileInfo
}

func (u *liveUpload) GetInfo(context.Context) (handler.FileInfo, error) {
	info := u.info
	info.MetaData = maps.Clone(u.info.MetaData)
	return info, nil
}

func (u *liveUpload) WriteChunk(_ context.Context, _ int64, src io.Reader) (int64, error) {
	file, err := u.in.root.OpenFile(u.in.incomingPath(u.info.ID), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return 0, fmt.Errorf("open upload %s: %w", u.info.ID, err)
	}
	buffer := u.in.buffers.get()
	defer u.in.buffers.put(buffer)
	n, err := io.CopyBuffer(recordingWriter{file: file, in: u.in, transferID: u.info.ID}, src, *buffer)
	u.info.Offset += n
	return n, errors.Join(err, file.Close())
}

func (u *liveUpload) GetReader(context.Context) (io.ReadCloser, error) {
	return u.in.root.Open(u.in.incomingPath(u.info.ID))
}

func (u *liveUpload) FinishUpload(context.Context) error {
	return nil
}

func (u *liveUpload) Terminate(context.Context) error {
	return u.in.removeLeftovers(u.info.ID)
}

func (u *liveUpload) DeclareLength(context.Context, int64) error {
	return handler.ErrNotImplemented
}

type recordingWriter struct {
	file       *os.File
	in         *Inbox
	transferID string
}

func (w recordingWriter) Write(p []byte) (int, error) {
	n, err := w.file.Write(p)
	w.in.wrote(w.transferID, int64(n))
	return n, err
}

func (in *Inbox) register(info handler.FileInfo) bool {
	in.mu.Lock()
	defer in.mu.Unlock()
	u, ok := in.uploads[info.ID]
	if !ok {
		return false
	}
	u.info = &info
	u.done = info.Offset
	return true
}

func (in *Inbox) wrote(transferID string, n int64) {
	in.mu.Lock()
	defer in.mu.Unlock()
	if u, ok := in.uploads[transferID]; ok {
		u.done += n
	}
}

func (in *Inbox) liveInfo(transferID string) (info handler.FileInfo, isLive bool, err error) {
	info, isLive, isLoaded := in.registeredInfo(transferID)
	if !isLive || isLoaded {
		return info, isLive, nil
	}
	stored, err := in.readStoredInfo(transferID)
	if err != nil {
		return handler.FileInfo{}, false, err
	}
	return in.adopt(stored)
}

func (in *Inbox) registeredInfo(transferID string) (info handler.FileInfo, isLive, isLoaded bool) {
	in.mu.Lock()
	defer in.mu.Unlock()
	u, ok := in.uploads[transferID]
	if !ok || u.info == nil {
		return handler.FileInfo{}, ok, false
	}
	return u.snapshot(), true, true
}

func (in *Inbox) adopt(stored handler.FileInfo) (info handler.FileInfo, isLive bool, err error) {
	in.mu.Lock()
	defer in.mu.Unlock()
	u, ok := in.uploads[stored.ID]
	if !ok {
		return handler.FileInfo{}, false, nil
	}
	if u.info == nil {
		u.info = &stored
		u.done = stored.Offset
	}
	return u.snapshot(), true, nil
}

func (u *upload) snapshot() handler.FileInfo {
	info := *u.info
	info.MetaData = maps.Clone(u.info.MetaData)
	info.Offset = u.done
	return info
}

func (in *Inbox) readStoredInfo(transferID string) (handler.FileInfo, error) {
	encoded, err := in.root.ReadFile(in.incomingPath(transferID + infoSuffix))
	if err != nil {
		return handler.FileInfo{}, storedUploadError(transferID, err)
	}
	var info handler.FileInfo
	if err := json.Unmarshal(encoded, &info); err != nil {
		return handler.FileInfo{}, fmt.Errorf("decode upload info %s: %w", transferID, err)
	}
	data, err := in.root.Stat(in.incomingPath(transferID))
	if err != nil {
		return handler.FileInfo{}, storedUploadError(transferID, err)
	}
	info.ID = transferID
	info.Offset = data.Size()
	info.Storage = map[string]string{
		"Type":                       "filestore",
		filestore.StorageKeyPath:     filepath.Join(in.cfg.IncomingDir, transferID),
		filestore.StorageKeyInfoPath: filepath.Join(in.cfg.IncomingDir, transferID+infoSuffix),
	}
	return info, nil
}

func storedUploadError(transferID string, err error) error {
	if errors.Is(err, fs.ErrNotExist) {
		return handler.ErrNotFound
	}
	return fmt.Errorf("read upload %s: %w", transferID, err)
}

type bufferPool struct {
	bytes int
	pool  sync.Pool
}

func (p *bufferPool) get() *[]byte {
	if buffer, ok := p.pool.Get().(*[]byte); ok {
		return buffer
	}
	buffer := make([]byte, p.bytes)
	return &buffer
}

func (p *bufferPool) put(buffer *[]byte) {
	p.pool.Put(buffer)
}

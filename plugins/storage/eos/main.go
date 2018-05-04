package main

import (
	"context"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"

	"github.com/cernbox/gohub/goconfig"
	"github.com/cernbox/reva/api"
	"github.com/cernbox/reva/plugins/storage/eos/eosclient"
	"github.com/grpc-ecosystem/go-grpc-middleware/tags/zap"
	"go.uber.org/zap"
)

func RegisterConfig(gc *goconfig.GoConfig) {
	gc.Add("storage-plugin-eos-namespace", "/eos/scratch/user/", "the prefix that will be added to paths.")
	gc.Add("storage-plugin-eos-client-binary", "/usr/bin/eos", "the eos client binary.")
	gc.Add("storage-plugin-eos-xrdcopy-binary", "/usr/bin/xrdcopy", "the xrdcopy binary.")
	gc.Add("storage-plugin-eos-mgm-url", "root://example.org", "the url of the mgm node.")
	gc.Add("storage-plugin-eos-cache-directory", "", "directory to store cached information. Empty defaults to OS temporary directory.")
}

func New(gc *goconfig.GoConfig, logger *zap.Logger) (api.Storage, error) {
	eosClientOpts := &eosclient.Options{
		CacheDirectory: gc.GetString("storage-plugin-eos-cache-directory"),
		URL:            gc.GetString("storage-plugin-eos-mgm-url"),
		XrdcopyBinary:  gc.GetString("storage-plugin-eos-xrdcopy-binary"),
		EosBinary:      gc.GetString("storage-plugin-eos-client-binary"),
	}
	eosClient, err := eosclient.New(eosClientOpts)
	if err != nil {
		return nil, err
	}

	namespace := path.Clean(gc.GetString("storage-plugin-eos-namespace"))
	if !strings.HasPrefix(namespace, "/") {
		namespace = "/"
	}
	s := &eosStorage{
		c:          eosClient,
		logger:     logger,
		mountpoint: namespace,
	}

	return s, nil
}

func getUserFromContext(ctx context.Context) (*api.User, error) {
	u, ok := api.ContextGetUser(ctx)
	if !ok {
		return nil, api.NewError(api.ContextUserRequiredError)
	}
	return u, nil
}

type eosStorage struct {
	c          *eosclient.Client
	mountpoint string
	logger     *zap.Logger
}

func (fs *eosStorage) getInternalPath(ctx context.Context, p string) string {
	l := ctx_zap.Extract(ctx)
	internalPath := path.Join(fs.mountpoint, p)
	l.Debug("path conversion: external => internal", zap.String("external", p), zap.String("internal", internalPath))
	return internalPath
}

func (fs *eosStorage) removeNamespace(np string) string {
	p := strings.TrimPrefix(np, fs.mountpoint)
	if p == "" {
		p = "/"
	}
	fs.logger.Debug("path conversion: internal => external", zap.String("internal", np), zap.String("external", p))
	return p
}

func (fs *eosStorage) GetPathByID(ctx context.Context, id string) (string, error) {
	u, err := getUserFromContext(ctx)
	if err != nil {
		return "", err
	}

	// parts[0] = 868317, parts[1] = photos, ...
	parts := strings.Split(id, "/")
	fileId, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return "", err
	}

	eosFileInfo, err := fs.c.GetFileInfoByInode(ctx, u.AccountId, fileId)
	if err != nil {
		return "", err
	}

	fi := fs.convertToMetadata(eosFileInfo)
	return fi.Path, nil
}

func (fs *eosStorage) GetMetadata(ctx context.Context, path string) (*api.Metadata, error) {
	u, err := getUserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	path = fs.getInternalPath(ctx, path)
	eosFileInfo, err := fs.c.GetFileInfoByPath(ctx, u.AccountId, path)
	if err != nil {
		return nil, err
	}
	fi := fs.convertToMetadata(eosFileInfo)
	return fi, nil
}

func (fs *eosStorage) ListFolder(ctx context.Context, path string) ([]*api.Metadata, error) {
	u, err := getUserFromContext(ctx)
	if err != nil {
		return nil, err
	}

	path = fs.getInternalPath(ctx, path)
	eosFileInfos, err := fs.c.List(ctx, u.AccountId, path)
	if err != nil {
		return nil, err
	}
	finfos := []*api.Metadata{}
	for _, eosFileInfo := range eosFileInfos {
		finfos = append(finfos, fs.convertToMetadata(eosFileInfo))
	}
	return finfos, nil
}

func (fs *eosStorage) CreateDir(ctx context.Context, path string) error {
	u, err := getUserFromContext(ctx)
	if err != nil {
		return err
	}
	path = fs.getInternalPath(ctx, path)
	return fs.c.CreateDir(ctx, u.AccountId, path)
}

func (fs *eosStorage) Delete(ctx context.Context, path string) error {
	u, err := getUserFromContext(ctx)
	if err != nil {
		return err
	}
	path = fs.getInternalPath(ctx, path)
	return fs.c.Remove(ctx, u.AccountId, path)
}

func (fs *eosStorage) Move(ctx context.Context, oldPath, newPath string) error {
	u, err := getUserFromContext(ctx)
	if err != nil {
		return err
	}
	oldPath = fs.getInternalPath(ctx, oldPath)
	newPath = fs.getInternalPath(ctx, newPath)
	return fs.c.Rename(ctx, u.AccountId, oldPath, newPath)
}

func (fs *eosStorage) Download(ctx context.Context, path string) (io.ReadCloser, error) {
	u, err := getUserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	path = fs.getInternalPath(ctx, path)
	return fs.c.Read(ctx, u.AccountId, path)
}

func (fs *eosStorage) Upload(ctx context.Context, path string, r io.ReadCloser) error {
	u, err := getUserFromContext(ctx)
	if err != nil {
		return err
	}
	path = fs.getInternalPath(ctx, path)
	return fs.c.Write(ctx, u.AccountId, path, r)
}

func (fs *eosStorage) ListRevisions(ctx context.Context, path string) ([]*api.Revision, error) {
	u, err := getUserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	path = fs.getInternalPath(ctx, path)
	eosRevisions, err := fs.c.ListVersions(ctx, u.AccountId, path)
	if err != nil {
		return nil, err
	}
	revisions := []*api.Revision{}
	for _, eosRev := range eosRevisions {
		rev := fs.convertToRevision(eosRev)
		revisions = append(revisions, rev)
	}
	return revisions, nil
}

func (fs *eosStorage) DownloadRevision(ctx context.Context, path, revisionKey string) (io.ReadCloser, error) {
	u, err := getUserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	path = fs.getInternalPath(ctx, path)
	return fs.c.ReadVersion(ctx, u.AccountId, path, revisionKey)
}

func (fs *eosStorage) RestoreRevision(ctx context.Context, path, revisionKey string) error {
	u, err := getUserFromContext(ctx)
	if err != nil {
		return err
	}
	path = fs.getInternalPath(ctx, path)
	return fs.c.RollbackToVersion(ctx, u.AccountId, path, revisionKey)
}

func (fs *eosStorage) EmptyRecycle(ctx context.Context, path string) error {
	u, err := getUserFromContext(ctx)
	if err != nil {
		return err
	}
	return fs.c.PurgeDeletedEntries(ctx, u.AccountId)
}

func (fs *eosStorage) ListRecycle(ctx context.Context, path string) ([]*api.RecycleEntry, error) {
	u, err := getUserFromContext(ctx)
	if err != nil {
		return nil, err
	}
	eosDeletedEntries, err := fs.c.ListDeletedEntries(ctx, u.AccountId)
	if err != nil {
		return nil, err
	}
	recycleEntries := []*api.RecycleEntry{}
	for _, entry := range eosDeletedEntries {
		recycleEntry := fs.convertToRecycleEntry(entry)
		recycleEntries = append(recycleEntries, recycleEntry)
	}
	return recycleEntries, nil
}

func (fs *eosStorage) RestoreRecycleEntry(ctx context.Context, restoreKey string) error {
	u, err := getUserFromContext(ctx)
	if err != nil {
		return err
	}
	return fs.c.RestoreDeletedEntry(ctx, u.AccountId, restoreKey)
}

func (fs *eosStorage) convertToRecycleEntry(eosDeletedEntry *eosclient.DeletedEntry) *api.RecycleEntry {
	recycleEntry := &api.RecycleEntry{
		RestorePath: fs.removeNamespace(eosDeletedEntry.RestorePath),
		RestoreKey:  eosDeletedEntry.RestoreKey,
		Size:        eosDeletedEntry.Size,
		DelMtime:    eosDeletedEntry.DeletionMTime,
		IsDir:       eosDeletedEntry.IsDir,
	}
	return recycleEntry
}

func (fs *eosStorage) convertToRevision(eosFileInfo *eosclient.FileInfo) *api.Revision {
	md := fs.convertToMetadata(eosFileInfo)
	revision := &api.Revision{
		RevKey: path.Base(md.Path),
		Size:   md.Size,
		Mtime:  md.Mtime,
		IsDir:  md.IsDir,
	}
	return revision
}
func (fs *eosStorage) convertToMetadata(eosFileInfo *eosclient.FileInfo) *api.Metadata {
	finfo := new(api.Metadata)
	finfo.Id = fmt.Sprintf("%d", eosFileInfo.Inode)
	finfo.Path = fs.removeNamespace(eosFileInfo.File)
	finfo.Mtime = eosFileInfo.MTime
	finfo.IsDir = eosFileInfo.IsDir
	finfo.Etag = eosFileInfo.ETag
	if finfo.IsDir {
		finfo.Size = eosFileInfo.TreeSize
	} else {
		finfo.Size = eosFileInfo.Size
	}
	return finfo
}

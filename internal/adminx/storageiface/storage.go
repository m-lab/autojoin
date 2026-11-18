package storageiface

import (
	"context"

	"google.golang.org/api/storage/v1"
)

// iamPolicyVersion requests the policy format that can represent conditional
// bindings, so a read-modify-write never drops conditions set by others.
const iamPolicyVersion = 3

type storageImpl struct {
	sc *storage.Service
}

// NewStorage creates a new storage implementation for wrapping the storage.Service.
func NewStorage(sc *storage.Service) *storageImpl {
	return &storageImpl{sc: sc}
}

func (s *storageImpl) GetManagedFolder(ctx context.Context, bucket, folder string) (*storage.ManagedFolder, error) {
	return s.sc.ManagedFolders.Get(bucket, folder).Context(ctx).Do()
}

func (s *storageImpl) InsertManagedFolder(ctx context.Context, bucket string, folder *storage.ManagedFolder) (*storage.ManagedFolder, error) {
	return s.sc.ManagedFolders.Insert(bucket, folder).Context(ctx).Do()
}

func (s *storageImpl) GetManagedFolderIamPolicy(ctx context.Context, bucket, folder string) (*storage.Policy, error) {
	return s.sc.ManagedFolders.GetIamPolicy(bucket, folder).OptionsRequestedPolicyVersion(iamPolicyVersion).Context(ctx).Do()
}

func (s *storageImpl) SetManagedFolderIamPolicy(ctx context.Context, bucket, folder string, policy *storage.Policy) (*storage.Policy, error) {
	return s.sc.ManagedFolders.SetIamPolicy(bucket, folder, policy).Context(ctx).Do()
}

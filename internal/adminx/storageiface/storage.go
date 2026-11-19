package storageiface

import (
	"context"

	"google.golang.org/api/storage/v1"
)

// iamPolicyVersion requests the policy format that can represent conditional
// bindings, so a read-modify-write never drops conditions set by others.
const iamPolicyVersion = 3

// Storage implements the managed folder operations used by adminx on top of
// a storage.Service.
type Storage struct {
	sc *storage.Service
}

// NewStorage creates a new Storage wrapping the given storage.Service.
func NewStorage(sc *storage.Service) *Storage {
	return &Storage{sc: sc}
}

func (s *Storage) GetManagedFolder(ctx context.Context, bucket, folder string) (*storage.ManagedFolder, error) {
	return s.sc.ManagedFolders.Get(bucket, folder).Context(ctx).Do()
}

func (s *Storage) InsertManagedFolder(ctx context.Context, bucket string, folder *storage.ManagedFolder) (*storage.ManagedFolder, error) {
	return s.sc.ManagedFolders.Insert(bucket, folder).Context(ctx).Do()
}

func (s *Storage) GetManagedFolderIamPolicy(ctx context.Context, bucket, folder string) (*storage.Policy, error) {
	return s.sc.ManagedFolders.GetIamPolicy(bucket, folder).OptionsRequestedPolicyVersion(iamPolicyVersion).Context(ctx).Do()
}

func (s *Storage) SetManagedFolderIamPolicy(ctx context.Context, bucket, folder string, policy *storage.Policy) (*storage.Policy, error) {
	return s.sc.ManagedFolders.SetIamPolicy(bucket, folder, policy).Context(ctx).Do()
}

package service

import "context"

import "rabbit-hole-server/internal/domain"

type FolderService interface {
	CreateFolder(ctx context.Context, spaceID uint, name string) (*domain.Folder, error)
	GetAllFolders(ctx context.Context, spaceID uint) ([]domain.Folder, error)
	UpdateFolder(ctx context.Context, folderID uint, name *string) (*domain.Folder, error)
	DeleteFolder(ctx context.Context, folderID uint) error
	GetFolderByID(ctx context.Context, folderID uint) (*domain.Folder, error)
}

type folderService struct {
	repo domain.FolderRepository
}

func NewFolderService(repo domain.FolderRepository) FolderService {
	return &folderService{repo: repo}
}

func (s *folderService) CreateFolder(ctx context.Context, spaceID uint, name string) (*domain.Folder, error) {
	folder := &domain.Folder{SpaceID: spaceID, Name: name}
	if err := s.repo.Create(ctx, folder); err != nil {
		return nil, err
	}
	return folder, nil
}

func (s *folderService) GetAllFolders(ctx context.Context, spaceID uint) ([]domain.Folder, error) {
	return s.repo.GetAllBySpace(ctx, spaceID)
}

func (s *folderService) GetFolderByID(ctx context.Context, folderID uint) (*domain.Folder, error) {
	return s.repo.GetByID(ctx, folderID)
}

func (s *folderService) UpdateFolder(ctx context.Context, folderID uint, name *string) (*domain.Folder, error) {
	folder, err := s.repo.GetByID(ctx, folderID)
	if err != nil {
		return nil, err
	}
	if name != nil {
		folder.Name = *name
	}
	if err := s.repo.Update(ctx, folder); err != nil {
		return nil, err
	}
	return folder, nil
}

func (s *folderService) DeleteFolder(ctx context.Context, folderID uint) error {
	return s.repo.Delete(ctx, folderID)
}

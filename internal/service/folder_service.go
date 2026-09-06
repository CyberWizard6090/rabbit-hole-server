package service

import "rabbit-hole-server/internal/domain"

type FolderService interface {
	CreateFolder(spaceID uint, name string) (*domain.Folder, error)
	GetAllFolders(spaceID uint) ([]domain.Folder, error)
	UpdateFolder(folderID uint, name *string) (*domain.Folder, error)
	DeleteFolder(folderID uint) error
	GetFolderByID(folderID uint) (*domain.Folder, error)
}

type folderService struct {
	repo domain.FolderRepository
}

func NewFolderService(repo domain.FolderRepository) FolderService {
	return &folderService{repo: repo}
}

func (s *folderService) CreateFolder(spaceID uint, name string) (*domain.Folder, error) {
	folder := &domain.Folder{SpaceID: spaceID, Name: name}
	if err := s.repo.Create(folder); err != nil {
		return nil, err
	}
	return folder, nil
}

func (s *folderService) GetAllFolders(spaceID uint) ([]domain.Folder, error) {
	return s.repo.GetAllBySpace(spaceID)
}

func (s *folderService) GetFolderByID(folderID uint) (*domain.Folder, error) {
	return s.repo.GetByID(folderID)
}

func (s *folderService) UpdateFolder(folderID uint, name *string) (*domain.Folder, error) {
	folder, err := s.repo.GetByID(folderID)
	if err != nil {
		return nil, err
	}
	if name != nil {
		folder.Name = *name
	}
	if err := s.repo.Update(folder); err != nil {
		return nil, err
	}
	return folder, nil
}

func (s *folderService) DeleteFolder(folderID uint) error {
	return s.repo.Delete(folderID)
}

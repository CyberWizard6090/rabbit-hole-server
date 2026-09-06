package service

import "rabbit-hole-server/internal/domain"

type SpaceService interface {
	CreateSpace(workspaceID uint, name string, currency string, userID uint) (*domain.Space, error)
	GetSpaceByID(id uint) (*domain.Space, error)
	GetAllSpaces(workspaceID uint, limit, offset int) ([]domain.Space, int64, error)
	GetDashboard(spaceID uint) (*domain.Space, error)
	CreateList(spaceID uint, name string, parentStatusID *uint) (*domain.List, error)

	CreateListInFolder(spaceID, folderID uint, name string) (*domain.List, error)
	GetListsByFolder(folderID uint) ([]domain.List, error)
	UpdateList(listID uint, name *string) (*domain.List, error)
	DeleteList(listID uint) error
}

type spaceService struct {
	repo domain.SpaceRepository
}

func NewSpaceService(repo domain.SpaceRepository) SpaceService {
	return &spaceService{repo: repo}
}

func (s *spaceService) CreateSpace(workspaceID uint, name string, currency string, userID uint) (*domain.Space, error) {
	space := &domain.Space{
		WorkspaceID: workspaceID,
		Name:        name,
		Currency:    currency,
		OwnerID:     userID,
	}

	if err := s.repo.Create(space); err != nil {
		return nil, err
	}

	defaultList := &domain.List{SpaceID: space.ID, Name: "Основной список"}
	if err := s.repo.CreateList(defaultList); err != nil {
		return nil, err
	}

	defaultStatuses := []domain.TaskStatus{
		{SpaceID: space.ID, ListID: defaultList.ID, Name: "К выполнению", Color: "#94A3B8", Position: 1, Type: domain.StatusTodo},
		{SpaceID: space.ID, ListID: defaultList.ID, Name: "В работе", Color: "#3B82F6", Position: 2, Type: domain.StatusInProgress},
		{SpaceID: space.ID, ListID: defaultList.ID, Name: "Готово", Color: "#22C55E", Position: 3, Type: domain.StatusDone},
	}
	for i := range defaultStatuses {
		if err := s.repo.CreateStatus(&defaultStatuses[i]); err != nil {
			return nil, err
		}
	}

	return space, nil
}

func (s *spaceService) GetSpaceByID(id uint) (*domain.Space, error) {
	return s.repo.GetByID(id)
}

func (s *spaceService) GetAllSpaces(workspaceID uint, limit, offset int) ([]domain.Space, int64, error) {
	return s.repo.GetAll(workspaceID, limit, offset)
}

func (s *spaceService) GetDashboard(spaceID uint) (*domain.Space, error) {
	return s.repo.GetDashboard(spaceID)
}

func (s *spaceService) CreateList(spaceID uint, name string, parentStatusID *uint) (*domain.List, error) {
	list := &domain.List{SpaceID: spaceID, Name: name, ParentStatusID: parentStatusID}
	if err := s.repo.CreateList(list); err != nil {
		return nil, err
	}
	return list, nil
}

func (s *spaceService) CreateListInFolder(spaceID, folderID uint, name string) (*domain.List, error) {
	list := &domain.List{SpaceID: spaceID, FolderID: &folderID, Name: name}
	if err := s.repo.CreateList(list); err != nil {
		return nil, err
	}
	return list, nil
}

func (s *spaceService) GetListsByFolder(folderID uint) ([]domain.List, error) {
	return s.repo.GetListsByFolder(folderID)
}

func (s *spaceService) UpdateList(listID uint, name *string) (*domain.List, error) {
	list, err := s.repo.GetListByID(listID)
	if err != nil {
		return nil, err
	}
	if name != nil {
		list.Name = *name
	}
	if err := s.repo.UpdateList(list); err != nil {
		return nil, err
	}
	return list, nil
}

func (s *spaceService) DeleteList(listID uint) error {
	return s.repo.DeleteList(listID)
}

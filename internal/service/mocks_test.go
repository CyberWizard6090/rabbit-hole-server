package service

import "rabbit-hole-server/internal/domain"

type contactRepositoryMock struct {
	contacts       []domain.User
	searchResults  []domain.User
	getErr         error
	addErr         error
	deleteErr      error
	searchErr      error
	gotGetUserID   uint
	gotAddUserID   uint
	gotAddContact  uint
	gotDeleteUser  uint
	gotDeleteCont  uint
	gotSearchQuery string
	gotExcludeID   uint

	getContactsFn   func(uint) ([]domain.User, error)
	addContactFn    func(uint, uint) error
	deleteContactFn func(uint, uint) error
	searchUsersFn   func(string, uint) ([]domain.User, error)
}

func (m *contactRepositoryMock) GetContacts(userID uint) ([]domain.User, error) {
	m.gotGetUserID = userID
	if m.getContactsFn != nil {
		return m.getContactsFn(userID)
	}
	return m.contacts, m.getErr
}
func (m *contactRepositoryMock) AddContact(userID, contactID uint) error {
	m.gotAddUserID, m.gotAddContact = userID, contactID
	if m.addContactFn != nil {
		return m.addContactFn(userID, contactID)
	}
	return m.addErr
}
func (m *contactRepositoryMock) DeleteContact(userID, contactID uint) error {
	m.gotDeleteUser, m.gotDeleteCont = userID, contactID
	if m.deleteContactFn != nil {
		return m.deleteContactFn(userID, contactID)
	}
	return m.deleteErr
}
func (m *contactRepositoryMock) SearchUsers(query string, excludeID uint) ([]domain.User, error) {
	m.gotSearchQuery, m.gotExcludeID = query, excludeID
	if m.searchUsersFn != nil {
		return m.searchUsersFn(query, excludeID)
	}
	return m.searchResults, m.searchErr
}

type folderRepositoryMock struct {
	folder        *domain.Folder
	folders       []domain.Folder
	getErr        error
	createErr     error
	updateErr     error
	deleteErr     error
	getAllErr     error
	createdFolder *domain.Folder
	updatedFolder *domain.Folder
	deletedID     uint

	createFn        func(*domain.Folder) error
	getByIDFn       func(uint) (*domain.Folder, error)
	getAllBySpaceFn func(uint) ([]domain.Folder, error)
	updateFn        func(*domain.Folder) error
	deleteFn        func(uint) error
}

func (m *folderRepositoryMock) Create(folder *domain.Folder) error {
	m.createdFolder = folder
	if m.createFn != nil {
		return m.createFn(folder)
	}
	return m.createErr
}
func (m *folderRepositoryMock) GetByID(id uint) (*domain.Folder, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id)
	}
	return m.folder, m.getErr
}
func (m *folderRepositoryMock) GetAllBySpace(spaceID uint) ([]domain.Folder, error) {
	if m.getAllBySpaceFn != nil {
		return m.getAllBySpaceFn(spaceID)
	}
	return m.folders, m.getAllErr
}
func (m *folderRepositoryMock) Update(folder *domain.Folder) error {
	m.updatedFolder = folder
	if m.updateFn != nil {
		return m.updateFn(folder)
	}
	return m.updateErr
}
func (m *folderRepositoryMock) Delete(id uint) error {
	m.deletedID = id
	if m.deleteFn != nil {
		return m.deleteFn(id)
	}
	return m.deleteErr
}

type spaceRepositoryMock struct {
	space             *domain.Space
	spaces            []domain.Space
	spaceCount        int64
	list              *domain.List
	lists             []domain.List
	statuses          []domain.TaskStatus
	getSpaceErr       error
	createSpaceErr    error
	getAllErr         error
	getDashboardErr   error
	createListErr     error
	getListErr        error
	getListsSpaceErr  error
	getStatusErr      error
	createStatusErr   error
	createTagErr      error
	getListsFolderErr error
	updateListErr     error
	deleteListErr     error
	createdSpace      *domain.Space
	createdList       *domain.List
	createdStatuses   []*domain.TaskStatus
	updatedList       *domain.List
	deletedListID     uint

	createFn           func(*domain.Space) error
	getByIDFn          func(uint) (*domain.Space, error)
	getAllFn           func(uint, int, int) ([]domain.Space, int64, error)
	dashboardFn        func(uint) (*domain.Space, error)
	createListFn       func(*domain.List) error
	getListByIDFn      func(uint) (*domain.List, error)
	getListsBySpaceFn  func(uint) ([]domain.List, error)
	getStatusByIDFn    func(uint) (*domain.TaskStatus, error)
	updateStatusFn     func(*domain.TaskStatus) error
	createStatusFn     func(*domain.TaskStatus) error
	createTagFn        func(*domain.Tag) error
	getListsByFolderFn func(uint) ([]domain.List, error)
	updateListFn       func(*domain.List) error
	deleteListFn       func(uint) error
}

func newMockSpaceRepository() *spaceRepositoryMock { return &spaceRepositoryMock{} }

func (m *spaceRepositoryMock) GetByID(id uint) (*domain.Space, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id)
	}
	return m.space, m.getSpaceErr
}
func (m *spaceRepositoryMock) Create(space *domain.Space) error {
	m.createdSpace = space
	if m.createFn != nil {
		return m.createFn(space)
	}
	if m.createSpaceErr == nil && space.ID == 0 {
		space.ID = 100
	}
	return m.createSpaceErr
}
func (m *spaceRepositoryMock) GetAll(workspaceID uint, limit, offset int) ([]domain.Space, int64, error) {
	if m.getAllFn != nil {
		return m.getAllFn(workspaceID, limit, offset)
	}
	return m.spaces, m.spaceCount, m.getAllErr
}
func (m *spaceRepositoryMock) GetDashboard(spaceID uint) (*domain.Space, error) {
	if m.dashboardFn != nil {
		return m.dashboardFn(spaceID)
	}
	return m.space, m.getDashboardErr
}
func (m *spaceRepositoryMock) CreateStatus(status *domain.TaskStatus) error {
	m.createdStatuses = append(m.createdStatuses, status)
	if m.createStatusFn != nil {
		return m.createStatusFn(status)
	}
	if m.createStatusErr == nil && status.ID == 0 {
		status.ID = uint(len(m.createdStatuses))
	}
	return m.createStatusErr
}
func (m *spaceRepositoryMock) CreateTag(tag *domain.Tag) error {
	if m.createTagFn != nil {
		return m.createTagFn(tag)
	}
	return m.createTagErr
}
func (m *spaceRepositoryMock) CreateList(list *domain.List) error {
	m.createdList = list
	if m.createListFn != nil {
		return m.createListFn(list)
	}
	if m.createListErr == nil && list.ID == 0 {
		list.ID = 200
	}
	return m.createListErr
}
func (m *spaceRepositoryMock) GetListByID(id uint) (*domain.List, error) {
	if m.getListByIDFn != nil {
		return m.getListByIDFn(id)
	}
	return m.list, m.getListErr
}
func (m *spaceRepositoryMock) GetListsBySpace(spaceID uint) ([]domain.List, error) {
	if m.getListsBySpaceFn != nil {
		return m.getListsBySpaceFn(spaceID)
	}
	return m.lists, m.getListsSpaceErr
}
func (m *spaceRepositoryMock) GetStatusByID(id uint) (*domain.TaskStatus, error) {
	if m.getStatusByIDFn != nil {
		return m.getStatusByIDFn(id)
	}
	if len(m.statuses) == 0 {
		return nil, m.getStatusErr
	}
	return &m.statuses[0], m.getStatusErr
}
func (m *spaceRepositoryMock) UpdateStatus(status *domain.TaskStatus) error {
	if m.updateStatusFn != nil {
		return m.updateStatusFn(status)
	}
	return nil
}
func (m *spaceRepositoryMock) GetListsByFolder(folderID uint) ([]domain.List, error) {
	if m.getListsByFolderFn != nil {
		return m.getListsByFolderFn(folderID)
	}
	return m.lists, m.getListsFolderErr
}
func (m *spaceRepositoryMock) UpdateList(list *domain.List) error {
	m.updatedList = list
	if m.updateListFn != nil {
		return m.updateListFn(list)
	}
	return m.updateListErr
}
func (m *spaceRepositoryMock) DeleteList(id uint) error {
	m.deletedListID = id
	if m.deleteListFn != nil {
		return m.deleteListFn(id)
	}
	return m.deleteListErr
}

type statusRepositoryMock struct {
	statuses          []domain.TaskStatus
	createErr         error
	getAllErr         error
	getErr            error
	updateErr         error
	updatePositionErr error
	deleteErr         error
	updatedStatus     *domain.TaskStatus
	positionStatus    *domain.TaskStatus
	positionRequested int
	deletedListID     uint
	deletedStatusID   uint
	created           *domain.TaskStatus

	createFn         func(*domain.TaskStatus) error
	getAllByListFn   func(uint) ([]domain.TaskStatus, error)
	getByIDFn        func(uint) (*domain.TaskStatus, error)
	updateFn         func(*domain.TaskStatus) error
	updatePositionFn func(*domain.TaskStatus, int) error
	deleteFn         func(uint, uint) error
}

func newMockStatusRepository() *statusRepositoryMock { return &statusRepositoryMock{} }

func (m *statusRepositoryMock) Create(status *domain.TaskStatus) error {
	m.created = status
	if m.createFn != nil {
		return m.createFn(status)
	}
	return m.createErr
}
func (m *statusRepositoryMock) GetAllByList(listID uint) ([]domain.TaskStatus, error) {
	if m.getAllByListFn != nil {
		return m.getAllByListFn(listID)
	}
	return m.statuses, m.getAllErr
}
func (m *statusRepositoryMock) GetByID(statusID uint) (*domain.TaskStatus, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(statusID)
	}
	if len(m.statuses) == 0 {
		return nil, m.getErr
	}
	return &m.statuses[0], m.getErr
}
func (m *statusRepositoryMock) Update(status *domain.TaskStatus) error {
	m.updatedStatus = status
	if m.updateFn != nil {
		return m.updateFn(status)
	}
	return m.updateErr
}
func (m *statusRepositoryMock) UpdatePosition(status *domain.TaskStatus, position int) error {
	m.positionStatus = status
	m.positionRequested = position
	if m.updatePositionFn != nil {
		return m.updatePositionFn(status, position)
	}
	if m.updatePositionErr == nil {
		status.Position = position
	}
	return m.updatePositionErr
}
func (m *statusRepositoryMock) Delete(listID uint, statusID uint) error {
	m.deletedListID, m.deletedStatusID = listID, statusID
	if m.deleteFn != nil {
		return m.deleteFn(listID, statusID)
	}
	return m.deleteErr
}

type tagRepositoryMock struct {
	tag            *domain.Tag
	tags           []domain.Tag
	byID           map[uint]*domain.Tag
	createErr      error
	allErr         error
	byIDErr        error
	byNameErr      error
	updateErr      error
	deleteErr      error
	usageErr       error
	mergeErr       error
	usageByID      map[uint]int64
	created        *domain.Tag
	updated        *domain.Tag
	deletedSpaceID uint
	deletedTagID   uint
	mergedSpaceID  uint
	mergedSourceID uint
	mergedTargetID uint
}

func (m *tagRepositoryMock) Create(tag *domain.Tag) error {
	m.created = tag
	return m.createErr
}
func (m *tagRepositoryMock) GetAllBySpace(spaceID uint) ([]domain.Tag, error) {
	return m.tags, m.allErr
}
func (m *tagRepositoryMock) GetByID(tagID uint) (*domain.Tag, error) {
	if m.byID != nil {
		tag, ok := m.byID[tagID]
		if !ok {
			return nil, m.byIDErr
		}
		return tag, m.byIDErr
	}
	return m.tag, m.byIDErr
}
func (m *tagRepositoryMock) GetByName(spaceID uint, name string) (*domain.Tag, error) {
	return m.tag, m.byNameErr
}
func (m *tagRepositoryMock) Update(tag *domain.Tag) error {
	m.updated = tag
	return m.updateErr
}
func (m *tagRepositoryMock) Delete(spaceID, tagID uint) error {
	m.deletedSpaceID, m.deletedTagID = spaceID, tagID
	return m.deleteErr
}
func (m *tagRepositoryMock) CountUsage(tagID uint) (int64, error) {
	if m.usageByID != nil {
		return m.usageByID[tagID], m.usageErr
	}
	return 0, m.usageErr
}
func (m *tagRepositoryMock) Merge(spaceID, sourceTagID, targetTagID uint) error {
	m.mergedSpaceID, m.mergedSourceID, m.mergedTargetID = spaceID, sourceTagID, targetTagID
	return m.mergeErr
}

type taskRepositoryMock struct {
	task        *domain.Task
	tasks       []domain.Task
	total       int64
	getErr      error
	createErr   error
	updateErr   error
	deleteErr   error
	addTagsErr  error
	createdTask *domain.Task
	assigneeIDs []uint
	tagIDs      []uint
	updatedTask *domain.Task
	deletedID   uint
	addedTaskID uint
	addedTagIDs []uint
}

func (m *taskRepositoryMock) Create(task *domain.Task, assigneeIDs []uint, tagIDs []uint) error {
	m.createdTask = task
	m.assigneeIDs = append([]uint(nil), assigneeIDs...)
	m.tagIDs = append([]uint(nil), tagIDs...)
	return m.createErr
}
func (m *taskRepositoryMock) GetAll(listID uint, limit, offset int) ([]domain.Task, int64, error) {
	return m.tasks, m.total, m.getErr
}
func (m *taskRepositoryMock) GetByID(id uint) (*domain.Task, error) { return m.task, m.getErr }
func (m *taskRepositoryMock) Update(task *domain.Task) error {
	m.updatedTask = task
	return m.updateErr
}
func (m *taskRepositoryMock) Delete(id uint) error { m.deletedID = id; return m.deleteErr }
func (m *taskRepositoryMock) AddTags(taskID uint, tagIDs []uint) error {
	m.addedTaskID = taskID
	m.addedTagIDs = append([]uint(nil), tagIDs...)
	return m.addTagsErr
}

type userRepositoryMock struct {
	user      *domain.User
	getErr    error
	updateErr error
	updated   *domain.User

	getByIDFn func(uint) (*domain.User, error)
	updateFn  func(*domain.User) error
}

type mockUserRepository = userRepositoryMock

func (m *userRepositoryMock) GetByID(id uint) (*domain.User, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id)
	}
	return m.user, m.getErr
}
func (m *userRepositoryMock) Update(user *domain.User) error {
	m.updated = user
	if m.updateFn != nil {
		return m.updateFn(user)
	}
	return m.updateErr
}

type workspaceRepositoryMock struct {
	workspace  *domain.Workspace
	workspaces []domain.Workspace
	getErr     error
	createErr  error
	allErr     error
	created    *domain.Workspace
	ownerID    uint

	createFn        func(*domain.Workspace, uint) error
	getByIDFn       func(uint) (*domain.Workspace, error)
	getAllForUserFn func(uint) ([]domain.Workspace, error)
}

func (m *workspaceRepositoryMock) Create(workspace *domain.Workspace, ownerID uint) error {
	m.created, m.ownerID = workspace, ownerID
	if m.createFn != nil {
		return m.createFn(workspace, ownerID)
	}
	return m.createErr
}
func (m *workspaceRepositoryMock) GetByID(id uint) (*domain.Workspace, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id)
	}
	return m.workspace, m.getErr
}
func (m *workspaceRepositoryMock) GetAllForUser(userID uint) ([]domain.Workspace, error) {
	if m.getAllForUserFn != nil {
		return m.getAllForUserFn(userID)
	}
	return m.workspaces, m.allErr
}

type tagServiceMock struct {
	results map[string]*domain.Tag
	err     error
	calls   []string
}

func (m *tagServiceMock) CreateTag(spaceID uint, params CreateTagParams) (*domain.Tag, error) {
	return nil, nil
}
func (m *tagServiceMock) GetAllBySpace(spaceID uint) ([]TagWithUsage, error) { return nil, nil }
func (m *tagServiceMock) UpdateTag(spaceID, tagID uint, params UpdateTagParams) (*domain.Tag, error) {
	return nil, nil
}
func (m *tagServiceMock) DeleteTag(spaceID, tagID uint) error { return nil }
func (m *tagServiceMock) GetOrCreateByName(spaceID uint, name string, defaultColor string) (*domain.Tag, error) {
	m.calls = append(m.calls, name)
	if m.err != nil {
		return nil, m.err
	}
	if m.results != nil {
		if tag, ok := m.results[name]; ok {
			return tag, nil
		}
	}
	return &domain.Tag{ID: uint(len(m.calls)), SpaceID: spaceID, Name: name, Color: defaultColor}, nil
}
func (m *tagServiceMock) MergeTags(spaceID, sourceTagID, targetTagID uint) error { return nil }

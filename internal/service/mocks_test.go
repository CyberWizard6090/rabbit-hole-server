package service

import (
	"context"

	"rabbit-hole-server/internal/domain"
	"rabbit-hole-server/internal/http/pagination"
)

type contactRepositoryMock struct {
	err       error
	users     []domain.CommentUser
	requests  []domain.ContactRequest
	calls     []string
	gotUser   uint
	gotOther  uint
	gotIn     bool
	gotQuery  string
	gotLimit  int
	gotOffset int
}

func (m *contactRepositoryMock) record(name string, userID, otherID uint) error {
	m.calls = append(m.calls, name)
	m.gotUser, m.gotOther = userID, otherID
	return m.err
}

func (m *contactRepositoryMock) ListContacts(_ context.Context, userID uint, limit, offset int) ([]domain.CommentUser, int64, error) {
	m.gotLimit, m.gotOffset = limit, offset
	return m.users, int64(len(m.users)), m.record("list", userID, 0)
}

func (m *contactRepositoryMock) Request(_ context.Context, requesterID, targetID uint) error {
	return m.record("request", requesterID, targetID)
}

func (m *contactRepositoryMock) ListRequests(_ context.Context, userID uint, incoming bool, limit, offset int) ([]domain.ContactRequest, int64, error) {
	m.gotIn, m.gotLimit, m.gotOffset = incoming, limit, offset
	return m.requests, int64(len(m.requests)), m.record("requests", userID, 0)
}

func (m *contactRepositoryMock) Accept(_ context.Context, userID, requesterID uint) error {
	return m.record("accept", userID, requesterID)
}

func (m *contactRepositoryMock) Decline(_ context.Context, userID, requesterID uint) error {
	return m.record("decline", userID, requesterID)
}

func (m *contactRepositoryMock) Cancel(_ context.Context, userID, targetID uint) error {
	return m.record("cancel", userID, targetID)
}

func (m *contactRepositoryMock) Remove(_ context.Context, userID, contactID uint) error {
	return m.record("remove", userID, contactID)
}

func (m *contactRepositoryMock) SearchUsers(_ context.Context, query string, excludeID uint) ([]domain.CommentUser, error) {
	m.gotQuery = query
	return m.users, m.record("search", excludeID, 0)
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

func (m *folderRepositoryMock) Create(_ context.Context, folder *domain.Folder) error {
	m.createdFolder = folder
	if m.createFn != nil {
		return m.createFn(folder)
	}
	return m.createErr
}
func (m *folderRepositoryMock) GetByID(_ context.Context, id uint) (*domain.Folder, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id)
	}
	return m.folder, m.getErr
}
func (m *folderRepositoryMock) GetAllBySpace(_ context.Context, spaceID uint) ([]domain.Folder, error) {
	if m.getAllBySpaceFn != nil {
		return m.getAllBySpaceFn(spaceID)
	}
	return m.folders, m.getAllErr
}
func (m *folderRepositoryMock) Update(_ context.Context, folder *domain.Folder) error {
	m.updatedFolder = folder
	if m.updateFn != nil {
		return m.updateFn(folder)
	}
	return m.updateErr
}
func (m *folderRepositoryMock) Delete(_ context.Context, id uint) error {
	m.deletedID = id
	if m.deleteFn != nil {
		return m.deleteFn(id)
	}
	return m.deleteErr
}

type spaceRepositoryMock struct {
	createWithDefaultsFn func(*domain.Space, *domain.List, []domain.TaskStatus) error
	space                *domain.Space
	spaces               []domain.Space
	spaceCount           int64
	list                 *domain.List
	lists                []domain.List
	statuses             []domain.TaskStatus
	getSpaceErr          error
	createSpaceErr       error
	getAllErr            error
	getDashboardErr      error
	createListErr        error
	getListErr           error
	getListsSpaceErr     error
	getStatusErr         error
	createStatusErr      error
	createTagErr         error
	getListsFolderErr    error
	updateListErr        error
	deleteListErr        error
	createdSpace         *domain.Space
	createdList          *domain.List
	createdStatuses      []*domain.TaskStatus
	updatedList          *domain.List
	deletedListID        uint

	createFn           func(*domain.Space) error
	getByIDFn          func(uint) (*domain.Space, error)
	getAllFn           func(uint, int, int) ([]domain.Space, int64, error)
	dashboardFn        func(uint, int) (*domain.Space, error)
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

func (m *spaceRepositoryMock) GetByID(_ context.Context, id uint) (*domain.Space, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id)
	}
	return m.space, m.getSpaceErr
}
func (m *spaceRepositoryMock) Create(_ context.Context, space *domain.Space) error {
	m.createdSpace = space
	if m.createFn != nil {
		return m.createFn(space)
	}
	if m.createSpaceErr == nil && space.ID == 0 {
		space.ID = 100
	}
	return m.createSpaceErr
}
func (m *spaceRepositoryMock) CreateWithDefaults(_ context.Context, space *domain.Space, list *domain.List, statuses []domain.TaskStatus) error {
	if m.createWithDefaultsFn != nil {
		return m.createWithDefaultsFn(space, list, statuses)
	}
	if m.createSpaceErr != nil {
		return m.createSpaceErr
	}
	space.ID, list.ID, list.SpaceID = 100, 200, 100
	for i := range statuses {
		statuses[i].SpaceID, statuses[i].ListID = space.ID, list.ID
	}
	return nil
}
func (m *spaceRepositoryMock) GetAll(_ context.Context, workspaceID uint, limit, offset int) ([]domain.Space, int64, error) {
	if m.getAllFn != nil {
		return m.getAllFn(workspaceID, limit, offset)
	}
	return m.spaces, m.spaceCount, m.getAllErr
}
func (m *spaceRepositoryMock) GetDashboard(_ context.Context, spaceID uint, tasksPerList int) (*domain.Space, error) {
	if m.dashboardFn != nil {
		return m.dashboardFn(spaceID, tasksPerList)
	}
	return m.space, m.getDashboardErr
}
func (m *spaceRepositoryMock) CreateStatus(_ context.Context, status *domain.TaskStatus) error {
	m.createdStatuses = append(m.createdStatuses, status)
	if m.createStatusFn != nil {
		return m.createStatusFn(status)
	}
	if m.createStatusErr == nil && status.ID == 0 {
		status.ID = uint(len(m.createdStatuses))
	}
	return m.createStatusErr
}
func (m *spaceRepositoryMock) CreateTag(_ context.Context, tag *domain.Tag) error {
	if m.createTagFn != nil {
		return m.createTagFn(tag)
	}
	return m.createTagErr
}
func (m *spaceRepositoryMock) CreateList(_ context.Context, list *domain.List, _ []domain.TaskStatus) error {
	m.createdList = list
	if m.createListFn != nil {
		return m.createListFn(list)
	}
	if m.createListErr == nil && list.ID == 0 {
		list.ID = 200
	}
	return m.createListErr
}
func (m *spaceRepositoryMock) GetListByID(_ context.Context, id uint) (*domain.List, error) {
	if m.getListByIDFn != nil {
		return m.getListByIDFn(id)
	}
	return m.list, m.getListErr
}
func (m *spaceRepositoryMock) GetListsBySpace(_ context.Context, spaceID uint) ([]domain.List, error) {
	if m.getListsBySpaceFn != nil {
		return m.getListsBySpaceFn(spaceID)
	}
	return m.lists, m.getListsSpaceErr
}
func (m *spaceRepositoryMock) GetStatusByID(_ context.Context, id uint) (*domain.TaskStatus, error) {
	if m.getStatusByIDFn != nil {
		return m.getStatusByIDFn(id)
	}
	if len(m.statuses) == 0 {
		return nil, m.getStatusErr
	}
	return &m.statuses[0], m.getStatusErr
}
func (m *spaceRepositoryMock) UpdateStatus(_ context.Context, status *domain.TaskStatus) error {
	if m.updateStatusFn != nil {
		return m.updateStatusFn(status)
	}
	return nil
}
func (m *spaceRepositoryMock) GetListsByFolder(_ context.Context, folderID uint) ([]domain.List, error) {
	if m.getListsByFolderFn != nil {
		return m.getListsByFolderFn(folderID)
	}
	return m.lists, m.getListsFolderErr
}
func (m *spaceRepositoryMock) UpdateList(_ context.Context, list *domain.List) error {
	m.updatedList = list
	if m.updateListFn != nil {
		return m.updateListFn(list)
	}
	return m.updateListErr
}
func (m *spaceRepositoryMock) DeleteList(_ context.Context, id uint) error {
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

func (m *statusRepositoryMock) Create(_ context.Context, status *domain.TaskStatus) error {
	m.created = status
	if m.createFn != nil {
		return m.createFn(status)
	}
	return m.createErr
}
func (m *statusRepositoryMock) GetAllByList(_ context.Context, listID uint) ([]domain.TaskStatus, error) {
	if m.getAllByListFn != nil {
		return m.getAllByListFn(listID)
	}
	return m.statuses, m.getAllErr
}
func (m *statusRepositoryMock) GetByID(_ context.Context, statusID uint) (*domain.TaskStatus, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(statusID)
	}
	if len(m.statuses) == 0 {
		return nil, m.getErr
	}
	return &m.statuses[0], m.getErr
}
func (m *statusRepositoryMock) Update(_ context.Context, status *domain.TaskStatus) error {
	m.updatedStatus = status
	if m.updateFn != nil {
		return m.updateFn(status)
	}
	return m.updateErr
}
func (m *statusRepositoryMock) UpdatePosition(_ context.Context, status *domain.TaskStatus, position int) error {
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
func (m *statusRepositoryMock) Delete(_ context.Context, listID uint, statusID uint, replacementID *uint) error {
	m.deletedListID, m.deletedStatusID = listID, statusID
	if m.deleteFn != nil {
		return m.deleteFn(listID, statusID)
	}
	return m.deleteErr
}

type tagRepositoryMock struct {
	byNameFn       func(call int) (*domain.Tag, error)
	byNameCalls    int
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

func (m *tagRepositoryMock) Create(_ context.Context, tag *domain.Tag) error {
	m.created = tag
	return m.createErr
}
func (m *tagRepositoryMock) GetAllBySpace(_ context.Context, spaceID uint) ([]domain.Tag, error) {
	return m.tags, m.allErr
}
func (m *tagRepositoryMock) GetByID(_ context.Context, tagID uint) (*domain.Tag, error) {
	if m.byID != nil {
		tag, ok := m.byID[tagID]
		if !ok {
			return nil, m.byIDErr
		}
		return tag, m.byIDErr
	}
	return m.tag, m.byIDErr
}
func (m *tagRepositoryMock) GetByName(_ context.Context, spaceID uint, name string) (*domain.Tag, error) {
	m.byNameCalls++
	if m.byNameFn != nil {
		return m.byNameFn(m.byNameCalls)
	}
	return m.tag, m.byNameErr
}
func (m *tagRepositoryMock) Update(_ context.Context, tag *domain.Tag) error {
	m.updated = tag
	return m.updateErr
}
func (m *tagRepositoryMock) Delete(_ context.Context, spaceID, tagID uint) error {
	m.deletedSpaceID, m.deletedTagID = spaceID, tagID
	return m.deleteErr
}
func (m *tagRepositoryMock) CountUsageBySpace(_ context.Context, spaceID uint) (map[uint]int64, error) {
	return m.usageByID, m.usageErr
}
func (m *tagRepositoryMock) Merge(_ context.Context, spaceID, sourceTagID, targetTagID uint) error {
	m.mergedSpaceID, m.mergedSourceID, m.mergedTargetID = spaceID, sourceTagID, targetTagID
	return m.mergeErr
}

type taskRepositoryMock struct {
	replacedAssignees *[]uint
	task              *domain.Task
	tasks             []domain.Task
	hasNext           bool
	hasPrev           bool
	listQuery         domain.TaskQuery
	total             int64
	listParams        pagination.Params
	listCalled        bool
	getErr            error
	createErr         error
	updateErr         error
	deleteErr         error
	addTagsErr        error
	createdTask       *domain.Task
	assigneeIDs       []uint
	tagIDs            []uint
	updatedTask       *domain.Task
	deletedID         uint
	addedTaskID       uint
	addedTagIDs       []uint
	moveErr           error
	movedTarget       domain.TaskMove
}

func (m *taskRepositoryMock) Move(_ context.Context, id uint, target domain.TaskMove) (*domain.Task, error) {
	m.movedTarget = target
	if m.moveErr != nil {
		return nil, m.moveErr
	}
	return &domain.Task{Model: domain.Model{ID: id}, ListID: target.ListID, StatusID: target.StatusID}, nil
}

func (m *taskRepositoryMock) Create(_ context.Context, task *domain.Task, assigneeIDs []uint, tagIDs []uint) error {
	m.createdTask = task
	m.assigneeIDs = append([]uint(nil), assigneeIDs...)
	m.tagIDs = append([]uint(nil), tagIDs...)
	return m.createErr
}
func (m *taskRepositoryMock) Count(_ context.Context, _ uint, _ domain.TaskFilter) (int64, error) {
	return m.total, m.getErr
}
func (m *taskRepositoryMock) List(_ context.Context, _ uint, query domain.TaskQuery, params pagination.Params) ([]domain.Task, bool, bool, error) {
	m.listCalled = true
	m.listQuery = query
	m.listParams = params
	return m.tasks, m.hasNext, m.hasPrev, m.getErr
}
func (m *taskRepositoryMock) GetByID(_ context.Context, id uint) (*domain.Task, error) {
	return m.task, m.getErr
}
func (m *taskRepositoryMock) Update(_ context.Context, id uint, apply func(*domain.Task) error, addTagIDs, removeTagIDs []uint, replaceAssigneeIDs *[]uint) (*domain.Task, *domain.Task, error) {
	m.replacedAssignees = replaceAssigneeIDs
	m.updatedTask = m.task
	m.addedTaskID = id
	if len(addTagIDs) > 0 {
		m.addedTagIDs = append([]uint(nil), addTagIDs...)
	}
	if m.updateErr != nil {
		return nil, nil, m.updateErr
	}
	if m.addTagsErr != nil {
		return nil, nil, m.addTagsErr
	}
	before := *m.task
	if err := apply(m.task); err != nil {
		return nil, nil, err
	}
	return &before, m.task, nil
}
func (m *taskRepositoryMock) Delete(_ context.Context, id uint) error {
	m.deletedID = id
	return m.deleteErr
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

func (m *userRepositoryMock) GetByID(_ context.Context, id uint) (*domain.User, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id)
	}
	return m.user, m.getErr
}
func (m *userRepositoryMock) Update(_ context.Context, user *domain.User) error {
	m.updated = user
	if m.updateFn != nil {
		return m.updateFn(user)
	}
	return m.updateErr
}

type workspaceRepositoryMock struct {
	workspace *domain.Workspace
	getErr    error
	createErr error
	allErr    error
	created   *domain.Workspace
	ownerID   uint

	createFn      func(*domain.Workspace, uint) error
	getByIDFn     func(uint) (*domain.Workspace, error)
	listForUserFn func(uint) ([]domain.WorkspaceSummary, int64, error)
}

func (m *workspaceRepositoryMock) Create(_ context.Context, workspace *domain.Workspace, ownerID uint) error {
	m.created, m.ownerID = workspace, ownerID
	if m.createFn != nil {
		return m.createFn(workspace, ownerID)
	}
	return m.createErr
}
func (m *workspaceRepositoryMock) GetByID(_ context.Context, id uint) (*domain.Workspace, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(id)
	}
	return m.workspace, m.getErr
}
func (m *workspaceRepositoryMock) ListForUser(_ context.Context, userID uint, _, _ int) ([]domain.WorkspaceSummary, int64, error) {
	if m.listForUserFn != nil {
		return m.listForUserFn(userID)
	}
	return nil, 0, m.allErr
}

type tagServiceMock struct {
	results map[string]*domain.Tag
	err     error
	calls   []string
}

func (m *tagServiceMock) CreateTag(_ context.Context, spaceID uint, params CreateTagParams) (*domain.Tag, error) {
	return nil, nil
}
func (m *tagServiceMock) GetAllBySpace(_ context.Context, spaceID uint) ([]TagWithUsage, error) {
	return nil, nil
}
func (m *tagServiceMock) UpdateTag(_ context.Context, spaceID, tagID uint, params UpdateTagParams) (*domain.Tag, error) {
	return nil, nil
}
func (m *tagServiceMock) DeleteTag(_ context.Context, spaceID, tagID uint) error { return nil }
func (m *tagServiceMock) GetOrCreateByName(_ context.Context, spaceID uint, name string, defaultColor string) (*domain.Tag, error) {
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
func (m *tagServiceMock) MergeTags(_ context.Context, spaceID, sourceTagID, targetTagID uint) error {
	return nil
}

func (m *tagServiceMock) GetTag(_ context.Context, spaceID, tagID uint) (*TagWithUsage, error) {
	return nil, nil
}
func (m *workspaceRepositoryMock) ListMembers(_ context.Context, workspaceID uint) ([]domain.MemberDetail, error) {
	return nil, nil
}
func (m *workspaceRepositoryMock) GetMember(_ context.Context, workspaceID, userID uint) (*domain.Member, error) {
	return nil, nil
}
func (m *workspaceRepositoryMock) GetRoleByName(_ context.Context, workspaceID uint, name string) (*domain.Role, error) {
	return &domain.Role{}, nil
}
func (m *workspaceRepositoryMock) AddMember(_ context.Context, member *domain.Member) error {
	return nil
}
func (m *workspaceRepositoryMock) UpdateMemberRole(_ context.Context, workspaceID, userID, roleID uint) error {
	return nil
}
func (m *workspaceRepositoryMock) RemoveMember(_ context.Context, workspaceID, userID uint) error {
	return nil
}
func (m *workspaceRepositoryMock) IsContact(_ context.Context, userID, otherID uint) (bool, error) {
	return false, nil
}
func (m *workspaceRepositoryMock) Update(_ context.Context, workspace *domain.Workspace) error {
	return nil
}
func (m *workspaceRepositoryMock) Delete(_ context.Context, id uint) error         { return nil }
func (m *spaceRepositoryMock) Update(_ context.Context, space *domain.Space) error { return nil }
func (m *spaceRepositoryMock) Delete(_ context.Context, id uint) error             { return nil }

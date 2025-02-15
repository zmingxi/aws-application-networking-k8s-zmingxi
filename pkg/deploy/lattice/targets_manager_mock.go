// Code generated by MockGen. DO NOT EDIT.
// Source: ./pkg/deploy/lattice/targets_manager.go

// Package lattice is a generated GoMock package.
package lattice

import (
	context "context"
	reflect "reflect"

	lattice "github.com/aws/aws-application-networking-k8s/pkg/model/lattice"
	gomock "github.com/golang/mock/gomock"
)

// MockTargetsManager is a mock of TargetsManager interface.
type MockTargetsManager struct {
	ctrl     *gomock.Controller
	recorder *MockTargetsManagerMockRecorder
}

// MockTargetsManagerMockRecorder is the mock recorder for MockTargetsManager.
type MockTargetsManagerMockRecorder struct {
	mock *MockTargetsManager
}

// NewMockTargetsManager creates a new mock instance.
func NewMockTargetsManager(ctrl *gomock.Controller) *MockTargetsManager {
	mock := &MockTargetsManager{ctrl: ctrl}
	mock.recorder = &MockTargetsManagerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockTargetsManager) EXPECT() *MockTargetsManagerMockRecorder {
	return m.recorder
}

// Create mocks base method.
func (m *MockTargetsManager) Create(ctx context.Context, targets *lattice.Targets) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Create", ctx, targets)
	ret0, _ := ret[0].(error)
	return ret0
}

// Create indicates an expected call of Create.
func (mr *MockTargetsManagerMockRecorder) Create(ctx, targets interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Create", reflect.TypeOf((*MockTargetsManager)(nil).Create), ctx, targets)
}

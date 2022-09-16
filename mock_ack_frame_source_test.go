// Code generated by MockGen. DO NOT EDIT.
// Source: packet_packer.go

// Package quic is a generated GoMock package.
package quic

import (
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	protocol "github.com/fkwhite/quic-go/internal/protocol"
	wire "github.com/fkwhite/quic-go/internal/wire"
)

// MockAckFrameSource is a mock of AckFrameSource interface.
type MockAckFrameSource struct {
	ctrl     *gomock.Controller
	recorder *MockAckFrameSourceMockRecorder
}

// MockAckFrameSourceMockRecorder is the mock recorder for MockAckFrameSource.
type MockAckFrameSourceMockRecorder struct {
	mock *MockAckFrameSource
}

// NewMockAckFrameSource creates a new mock instance.
func NewMockAckFrameSource(ctrl *gomock.Controller) *MockAckFrameSource {
	mock := &MockAckFrameSource{ctrl: ctrl}
	mock.recorder = &MockAckFrameSourceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockAckFrameSource) EXPECT() *MockAckFrameSourceMockRecorder {
	return m.recorder
}

// GetAckFrame mocks base method.
func (m *MockAckFrameSource) GetAckFrame(encLevel protocol.EncryptionLevel, onlyIfQueued bool) *wire.AckFrame {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetAckFrame", encLevel, onlyIfQueued)
	ret0, _ := ret[0].(*wire.AckFrame)
	return ret0
}

// GetAckFrame indicates an expected call of GetAckFrame.
func (mr *MockAckFrameSourceMockRecorder) GetAckFrame(encLevel, onlyIfQueued interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetAckFrame", reflect.TypeOf((*MockAckFrameSource)(nil).GetAckFrame), encLevel, onlyIfQueued)
}

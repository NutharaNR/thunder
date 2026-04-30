/*
 * Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
 *
 * WSO2 LLC. licenses this file to you under the Apache License,
 * Version 2.0 (the "License"); you may not use this file except
 * in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

package executor

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/asgardeo/thunder/internal/flow/common"
	"github.com/asgardeo/thunder/internal/flow/core"
	"github.com/asgardeo/thunder/internal/system/config"
	"github.com/asgardeo/thunder/tests/mocks/flow/coremock"
)

type TokenLinkExecutorTestSuite struct {
	suite.Suite
	mockFlowFactory *coremock.FlowFactoryInterfaceMock
	executor        *tokenLinkExecutor
}

func (suite *TokenLinkExecutorTestSuite) SetupTest() {
	err := config.InitializeServerRuntime(".", &config.Config{
		GateClient: config.GateClientConfig{
			Scheme:   "https",
			Hostname: "localhost",
			Port:     5190,
			Path:     "/gate",
		},
	})
	suite.Require().NoError(err)

	suite.mockFlowFactory = coremock.NewFlowFactoryInterfaceMock(suite.T())
	mockBaseExecutor := coremock.NewExecutorInterfaceMock(suite.T())

	suite.mockFlowFactory.On("CreateExecutor",
		ExecutorNameTokenLinkExecutor,
		common.ExecutorTypeUtility,
		[]common.Input{},
		[]common.Input{}).Return(mockBaseExecutor)

	suite.executor = newTokenLinkExecutor(suite.mockFlowFactory)
}

func (suite *TokenLinkExecutorTestSuite) TearDownTest() {
	config.ResetServerRuntime()
}

// --- Generate mode: registration / invite flow ---

func (suite *TokenLinkExecutorTestSuite) TestExecute_GenerateMode_Registration_Success() {
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		AppID:        "test-app-id",
		FlowType:     common.FlowTypeRegistration,
		ExecutorMode: ExecutorModeGenerate,
		UserInputs:   make(map[string]string),
		RuntimeData:  make(map[string]string),
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), common.ExecComplete, resp.Status)
	assert.NotEmpty(suite.T(), resp.RuntimeData[common.RuntimeKeyStoredInviteToken])
	assert.NotEmpty(suite.T(), resp.RuntimeData[common.RuntimeKeyInviteLink])
	assert.Contains(suite.T(), resp.RuntimeData[common.RuntimeKeyInviteLink], "inviteToken=")
	assert.Contains(suite.T(), resp.RuntimeData[common.RuntimeKeyInviteLink], "executionId=test-execution-id")
	assert.Contains(suite.T(), resp.RuntimeData[common.RuntimeKeyInviteLink], "applicationId=test-app-id")
	assert.Empty(suite.T(), resp.AdditionalData[common.DataInviteLink])
}

func (suite *TokenLinkExecutorTestSuite) TestExecute_GenerateMode_UserOnboarding_ExposesLink() {
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		AppID:        "test-app-id",
		FlowType:     common.FlowTypeUserOnboarding,
		ExecutorMode: ExecutorModeGenerate,
		UserInputs:   make(map[string]string),
		RuntimeData:  make(map[string]string),
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), common.ExecComplete, resp.Status)
	assert.NotEmpty(suite.T(), resp.RuntimeData[common.RuntimeKeyInviteLink])
	assert.Equal(suite.T(), resp.RuntimeData[common.RuntimeKeyInviteLink], resp.AdditionalData[common.DataInviteLink])
}

func (suite *TokenLinkExecutorTestSuite) TestExecute_GenerateMode_Registration_Idempotency() {
	existingToken := "existing-token-123"
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		FlowType:     common.FlowTypeRegistration,
		ExecutorMode: ExecutorModeGenerate,
		UserInputs:   make(map[string]string),
		RuntimeData: map[string]string{
			common.RuntimeKeyStoredInviteToken: existingToken,
		},
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), common.ExecComplete, resp.Status)
	assert.Equal(suite.T(), existingToken, resp.RuntimeData[common.RuntimeKeyStoredInviteToken])
	assert.Contains(suite.T(), resp.RuntimeData[common.RuntimeKeyInviteLink], existingToken)
	assert.Empty(suite.T(), resp.AdditionalData[common.DataInviteLink])
}

func (suite *TokenLinkExecutorTestSuite) TestExecute_GenerateMode_Registration_PopulatesTemplateData() {
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		FlowType:     common.FlowTypeRegistration,
		ExecutorMode: ExecutorModeGenerate,
		RuntimeData:  make(map[string]string),
	}

	resp, err := suite.executor.Execute(ctx)

	suite.NoError(err)
	suite.Equal(common.ExecComplete, resp.Status)

	templateData, ok := resp.ForwardedData[common.ForwardedDataKeyTemplateData].(map[string]interface{})
	suite.True(ok, "Expected template data to be map[string]interface{}")
	suite.NotEmpty(templateData["inviteLink"], "inviteLink must be present in template data")
}

// --- Generate mode: recovery flow ---

func (suite *TokenLinkExecutorTestSuite) TestExecute_GenerateMode_Recovery_Success() {
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		AppID:        "test-app-id",
		FlowType:     common.FlowTypeRecovery,
		ExecutorMode: ExecutorModeGenerate,
		UserInputs:   make(map[string]string),
		RuntimeData:  make(map[string]string),
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), common.ExecComplete, resp.Status)
	assert.NotEmpty(suite.T(), resp.RuntimeData[common.RuntimeKeyStoredRecoveryToken])
	assert.NotEmpty(suite.T(), resp.RuntimeData[common.RuntimeKeyRecoveryLink])
	assert.Contains(suite.T(), resp.RuntimeData[common.RuntimeKeyRecoveryLink], "recoveryToken=")
	assert.Contains(suite.T(), resp.RuntimeData[common.RuntimeKeyRecoveryLink], "executionId=test-execution-id")
	assert.Contains(suite.T(), resp.RuntimeData[common.RuntimeKeyRecoveryLink], "applicationId=test-app-id")
}

func (suite *TokenLinkExecutorTestSuite) TestExecute_GenerateMode_Recovery_PopulatesTemplateData() {
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		FlowType:     common.FlowTypeRecovery,
		ExecutorMode: ExecutorModeGenerate,
		RuntimeData:  make(map[string]string),
	}

	resp, err := suite.executor.Execute(ctx)

	suite.NoError(err)
	suite.Equal(common.ExecComplete, resp.Status)

	templateData, ok := resp.ForwardedData[common.ForwardedDataKeyTemplateData].(map[string]interface{})
	suite.True(ok, "Expected template data to be map[string]interface{}")
	suite.NotEmpty(templateData["recoveryLink"], "recoveryLink must be present in template data")
}

// --- Verify mode: registration / invite flow ---

func (suite *TokenLinkExecutorTestSuite) TestExecute_VerifyMode_Registration_NoTokenProvided() {
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		FlowType:     common.FlowTypeRegistration,
		ExecutorMode: ExecutorModeVerify,
		UserInputs:   make(map[string]string),
		RuntimeData: map[string]string{
			common.RuntimeKeyStoredInviteToken: "stored-token",
		},
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), common.ExecUserInputRequired, resp.Status)
}

func (suite *TokenLinkExecutorTestSuite) TestExecute_VerifyMode_Registration_ValidationSuccess() {
	token := "valid-invite-token"
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		FlowType:     common.FlowTypeRegistration,
		ExecutorMode: ExecutorModeVerify,
		UserInputs: map[string]string{
			userInputInviteToken: token,
		},
		RuntimeData: map[string]string{
			common.RuntimeKeyStoredInviteToken: token,
		},
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), common.ExecComplete, resp.Status)
}

func (suite *TokenLinkExecutorTestSuite) TestExecute_VerifyMode_Registration_TokenMismatch() {
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		FlowType:     common.FlowTypeRegistration,
		ExecutorMode: ExecutorModeVerify,
		UserInputs: map[string]string{
			userInputInviteToken: "wrong-token",
		},
		RuntimeData: map[string]string{
			common.RuntimeKeyStoredInviteToken: "correct-token",
		},
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), common.ExecFailure, resp.Status)
	assert.Equal(suite.T(), "Invalid token", resp.FailureReason)
}

func (suite *TokenLinkExecutorTestSuite) TestExecute_VerifyMode_Registration_NoStoredToken() {
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		FlowType:     common.FlowTypeRegistration,
		ExecutorMode: ExecutorModeVerify,
		UserInputs: map[string]string{
			userInputInviteToken: "some-token",
		},
		RuntimeData: make(map[string]string),
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), common.ExecFailure, resp.Status)
	assert.Equal(suite.T(), "Invalid token", resp.FailureReason)
}

// --- Verify mode: recovery flow ---

func (suite *TokenLinkExecutorTestSuite) TestExecute_VerifyMode_Recovery_NoTokenProvided() {
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		FlowType:     common.FlowTypeRecovery,
		ExecutorMode: ExecutorModeVerify,
		UserInputs:   make(map[string]string),
		RuntimeData: map[string]string{
			common.RuntimeKeyStoredRecoveryToken: "stored-token",
		},
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), common.ExecUserInputRequired, resp.Status)
}

func (suite *TokenLinkExecutorTestSuite) TestExecute_VerifyMode_Recovery_ValidationSuccess() {
	token := "valid-recovery-token"
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		FlowType:     common.FlowTypeRecovery,
		ExecutorMode: ExecutorModeVerify,
		UserInputs: map[string]string{
			userInputRecoveryToken: token,
		},
		RuntimeData: map[string]string{
			common.RuntimeKeyStoredRecoveryToken: token,
		},
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), common.ExecComplete, resp.Status)
}

func (suite *TokenLinkExecutorTestSuite) TestExecute_VerifyMode_Recovery_TokenMismatch() {
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		FlowType:     common.FlowTypeRecovery,
		ExecutorMode: ExecutorModeVerify,
		UserInputs: map[string]string{
			userInputRecoveryToken: "wrong-token",
		},
		RuntimeData: map[string]string{
			common.RuntimeKeyStoredRecoveryToken: "correct-token",
		},
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), common.ExecFailure, resp.Status)
	assert.Equal(suite.T(), "Invalid token", resp.FailureReason)
}

func (suite *TokenLinkExecutorTestSuite) TestExecute_VerifyMode_Recovery_NoStoredToken() {
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		FlowType:     common.FlowTypeRecovery,
		ExecutorMode: ExecutorModeVerify,
		UserInputs: map[string]string{
			userInputRecoveryToken: "some-token",
		},
		RuntimeData: make(map[string]string),
	}

	resp, err := suite.executor.Execute(ctx)

	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), common.ExecFailure, resp.Status)
	assert.Equal(suite.T(), "Invalid token", resp.FailureReason)
}

// --- Error cases ---

func (suite *TokenLinkExecutorTestSuite) TestExecute_InvalidMode() {
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		FlowType:     common.FlowTypeRegistration,
		ExecutorMode: "invalid",
		UserInputs:   make(map[string]string),
		RuntimeData:  make(map[string]string),
	}

	resp, err := suite.executor.Execute(ctx)

	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), resp)
	assert.Contains(suite.T(), err.Error(), "invalid executor mode for TokenLinkExecutor")
}

func (suite *TokenLinkExecutorTestSuite) TestExecute_UnsupportedFlowType() {
	ctx := &core.NodeContext{
		ExecutionID:  "test-execution-id",
		FlowType:     common.FlowTypeAuthentication,
		ExecutorMode: ExecutorModeGenerate,
		UserInputs:   make(map[string]string),
		RuntimeData:  make(map[string]string),
	}

	resp, err := suite.executor.Execute(ctx)

	assert.Error(suite.T(), err)
	assert.Nil(suite.T(), resp)
	assert.Contains(suite.T(), err.Error(), "unsupported flow type for TokenLinkExecutor")
}

// --- Execution policy ---

func (suite *TokenLinkExecutorTestSuite) TestGetExecutionPolicy_GenerateMode_ReturnsNil() {
	policy := suite.executor.GetExecutionPolicy(ExecutorModeGenerate)
	assert.Nil(suite.T(), policy)
}

func (suite *TokenLinkExecutorTestSuite) TestGetExecutionPolicy_VerifyMode_SkipsChallengeValidation() {
	policy := suite.executor.GetExecutionPolicy(ExecutorModeVerify)
	assert.NotNil(suite.T(), policy)
	assert.True(suite.T(), policy.SkipChallengeValidation)
	assert.True(suite.T(), policy.AllowSegmentRestart)
}

func (suite *TokenLinkExecutorTestSuite) TestGetExecutionPolicy_InvalidMode_ReturnsNil() {
	policy := suite.executor.GetExecutionPolicy("invalid-mode")
	assert.Nil(suite.T(), policy)
}

func (suite *TokenLinkExecutorTestSuite) TestGetExecutionPolicy_EmptyMode_ReturnsNil() {
	policy := suite.executor.GetExecutionPolicy("")
	assert.Nil(suite.T(), policy)
}

func TestTokenLinkExecutorSuite(t *testing.T) {
	suite.Run(t, new(TokenLinkExecutorTestSuite))
}

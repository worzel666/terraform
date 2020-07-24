package s3

import (
	"fmt"
	"net/url"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/s3"
	awsbase "github.com/hashicorp/aws-sdk-go-base"
	"github.com/hashicorp/terraform/backend"
	"github.com/hashicorp/terraform/configs/hcl2shim"
	"github.com/hashicorp/terraform/state/remote"
	"github.com/hashicorp/terraform/states"
)

const (
	mockStsAssumeRoleArn    = `arn:aws:iam::555555555555:role/AssumeRole`
	mockStsAssumeRolePolicy = `{
  "Version": "2012-10-17",
  "Statement": {
    "Effect": "Allow",
    "Action": "*",
    "Resource": "*",
  }
}`
	mockStsAssumeRolePolicyArn        = `arn:aws:iam::555555555555:policy/AssumeRolePolicy1`
	mockStsAssumeRoleSessionName      = `AssumeRoleSessionName`
	mockStsAssumeRoleTagKey           = `AssumeRoleTagKey`
	mockStsAssumeRoleTagValue         = `AssumeRoleTagValue`
	mockStsAssumeRoleTransitiveTagKey = `AssumeRoleTagKey`
	mockStsAssumeRoleValidResponse    = `<AssumeRoleResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
<AssumeRoleResult>
  <AssumedRoleUser>
    <Arn>arn:aws:sts::555555555555:assumed-role/role/AssumeRoleSessionName</Arn>
    <AssumedRoleId>ARO123EXAMPLE123:AssumeRoleSessionName</AssumedRoleId>
  </AssumedRoleUser>
  <Credentials>
    <AccessKeyId>AssumeRoleAccessKey</AccessKeyId>
    <SecretAccessKey>AssumeRoleSecretKey</SecretAccessKey>
    <SessionToken>AssumeRoleSessionToken</SessionToken>
    <Expiration>2099-12-31T23:59:59Z</Expiration>
  </Credentials>
</AssumeRoleResult>
<ResponseMetadata>
  <RequestId>01234567-89ab-cdef-0123-456789abcdef</RequestId>
</ResponseMetadata>
</AssumeRoleResponse>`
	mockStsGetCallerIdentityValidResponseBody = `<GetCallerIdentityResponse xmlns="https://sts.amazonaws.com/doc/2011-06-15/">
  <GetCallerIdentityResult>
   <Arn>arn:aws:iam::222222222222:user/Alice</Arn>
    <UserId>AKIAI44QH8DHBEXAMPLE</UserId>
    <Account>222222222222</Account>
  </GetCallerIdentityResult>
  <ResponseMetadata>
    <RequestId>01234567-89ab-cdef-0123-456789abcdef</RequestId>
  </ResponseMetadata>
</GetCallerIdentityResponse>`
)

var (
	mockStsGetCallerIdentityRequestBody = url.Values{
		"Action":  []string{"GetCallerIdentity"},
		"Version": []string{"2011-06-15"},
	}.Encode()
)

// verify that we are doing ACC tests or the S3 tests specifically
func testACC(t *testing.T) {
	skip := os.Getenv("TF_ACC") == "" && os.Getenv("TF_S3_TEST") == ""
	if skip {
		t.Log("s3 backend tests require setting TF_ACC or TF_S3_TEST")
		t.Skip()
	}
	if os.Getenv("AWS_DEFAULT_REGION") == "" {
		os.Setenv("AWS_DEFAULT_REGION", "us-west-2")
	}
}

func TestBackend_impl(t *testing.T) {
	var _ backend.Backend = new(Backend)
}

func TestBackendConfig(t *testing.T) {
	testACC(t)
	config := map[string]interface{}{
		"region":         "us-west-1",
		"bucket":         "tf-test",
		"key":            "state",
		"encrypt":        true,
		"dynamodb_table": "dynamoTable",
	}

	b := backend.TestBackendConfig(t, New(), backend.TestWrapConfig(config)).(*Backend)

	if *b.s3Client.Config.Region != "us-west-1" {
		t.Fatalf("Incorrect region was populated")
	}
	if b.bucketName != "tf-test" {
		t.Fatalf("Incorrect bucketName was populated")
	}
	if b.keyName != "state" {
		t.Fatalf("Incorrect keyName was populated")
	}

	credentials, err := b.s3Client.Config.Credentials.Get()
	if err != nil {
		t.Fatalf("Error when requesting credentials")
	}
	if credentials.AccessKeyID == "" {
		t.Fatalf("No Access Key Id was populated")
	}
	if credentials.SecretAccessKey == "" {
		t.Fatalf("No Secret Access Key was populated")
	}
}

func TestBackendConfig_AssumeRole(t *testing.T) {
	testACC(t)

	testCases := []struct {
		Config           map[string]interface{}
		Description      string
		MockStsEndpoints []*awsbase.MockEndpoint
	}{
		{
			Config: map[string]interface{}{
				"bucket":       "tf-test",
				"key":          "state",
				"region":       "us-west-1",
				"role_arn":     mockStsAssumeRoleArn,
				"session_name": mockStsAssumeRoleSessionName,
			},
			Description: "role_arn",
			MockStsEndpoints: []*awsbase.MockEndpoint{
				{
					Request: &awsbase.MockRequest{Method: "POST", Uri: "/", Body: url.Values{
						"Action":          []string{"AssumeRole"},
						"DurationSeconds": []string{"900"},
						"RoleArn":         []string{mockStsAssumeRoleArn},
						"RoleSessionName": []string{mockStsAssumeRoleSessionName},
						"Version":         []string{"2011-06-15"},
					}.Encode()},
					Response: &awsbase.MockResponse{StatusCode: 200, Body: mockStsAssumeRoleValidResponse, ContentType: "text/xml"},
				},
				{
					Request:  &awsbase.MockRequest{Method: "POST", Uri: "/", Body: mockStsGetCallerIdentityRequestBody},
					Response: &awsbase.MockResponse{StatusCode: 200, Body: mockStsGetCallerIdentityValidResponseBody, ContentType: "text/xml"},
				},
			},
		},
		{
			Config: map[string]interface{}{
				"assume_role_duration_seconds": 3600,
				"bucket":                       "tf-test",
				"key":                          "state",
				"region":                       "us-west-1",
				"role_arn":                     mockStsAssumeRoleArn,
				"session_name":                 mockStsAssumeRoleSessionName,
			},
			Description: "assume_role_duration_seconds",
			MockStsEndpoints: []*awsbase.MockEndpoint{
				{
					Request: &awsbase.MockRequest{"POST", "/", url.Values{
						"Action":          []string{"AssumeRole"},
						"DurationSeconds": []string{"3600"},
						"RoleArn":         []string{mockStsAssumeRoleArn},
						"RoleSessionName": []string{mockStsAssumeRoleSessionName},
						"Version":         []string{"2011-06-15"},
					}.Encode()},
					Response: &awsbase.MockResponse{StatusCode: 200, Body: mockStsAssumeRoleValidResponse, ContentType: "text/xml"},
				},
				{
					Request:  &awsbase.MockRequest{Method: "POST", Uri: "/", Body: mockStsGetCallerIdentityRequestBody},
					Response: &awsbase.MockResponse{StatusCode: 200, Body: mockStsGetCallerIdentityValidResponseBody, ContentType: "text/xml"},
				},
			},
		},
		{
			Config: map[string]interface{}{
				"bucket":       "tf-test",
				"external_id":  "AssumeRoleExternalId",
				"key":          "state",
				"region":       "us-west-1",
				"role_arn":     mockStsAssumeRoleArn,
				"session_name": mockStsAssumeRoleSessionName,
			},
			Description: "external_id",
			MockStsEndpoints: []*awsbase.MockEndpoint{
				{
					Request: &awsbase.MockRequest{"POST", "/", url.Values{
						"Action":          []string{"AssumeRole"},
						"DurationSeconds": []string{"900"},
						"ExternalId":      []string{"AssumeRoleExternalId"},
						"RoleArn":         []string{mockStsAssumeRoleArn},
						"RoleSessionName": []string{mockStsAssumeRoleSessionName},
						"Version":         []string{"2011-06-15"},
					}.Encode()},
					Response: &awsbase.MockResponse{StatusCode: 200, Body: mockStsAssumeRoleValidResponse, ContentType: "text/xml"},
				},
				{
					Request:  &awsbase.MockRequest{Method: "POST", Uri: "/", Body: mockStsGetCallerIdentityRequestBody},
					Response: &awsbase.MockResponse{StatusCode: 200, Body: mockStsGetCallerIdentityValidResponseBody, ContentType: "text/xml"},
				},
			},
		},
		{
			Config: map[string]interface{}{
				"assume_role_policy": mockStsAssumeRolePolicy,
				"bucket":             "tf-test",
				"key":                "state",
				"region":             "us-west-1",
				"role_arn":           mockStsAssumeRoleArn,
				"session_name":       mockStsAssumeRoleSessionName,
			},
			Description: "assume_role_policy",
			MockStsEndpoints: []*awsbase.MockEndpoint{
				{
					Request: &awsbase.MockRequest{"POST", "/", url.Values{
						"Action":          []string{"AssumeRole"},
						"DurationSeconds": []string{"900"},
						"Policy":          []string{mockStsAssumeRolePolicy},
						"RoleArn":         []string{mockStsAssumeRoleArn},
						"RoleSessionName": []string{mockStsAssumeRoleSessionName},
						"Version":         []string{"2011-06-15"},
					}.Encode()},
					Response: &awsbase.MockResponse{StatusCode: 200, Body: mockStsAssumeRoleValidResponse, ContentType: "text/xml"},
				},
				{
					Request:  &awsbase.MockRequest{Method: "POST", Uri: "/", Body: mockStsGetCallerIdentityRequestBody},
					Response: &awsbase.MockResponse{StatusCode: 200, Body: mockStsGetCallerIdentityValidResponseBody, ContentType: "text/xml"},
				},
			},
		},
		{
			Config: map[string]interface{}{
				"assume_role_policy_arns": []interface{}{mockStsAssumeRolePolicyArn},
				"bucket":                  "tf-test",
				"key":                     "state",
				"region":                  "us-west-1",
				"role_arn":                mockStsAssumeRoleArn,
				"session_name":            mockStsAssumeRoleSessionName,
			},
			Description: "assume_role_policy_arns",
			MockStsEndpoints: []*awsbase.MockEndpoint{
				{
					Request: &awsbase.MockRequest{Method: "POST", Uri: "/", Body: url.Values{
						"Action":                  []string{"AssumeRole"},
						"DurationSeconds":         []string{"900"},
						"PolicyArns.member.1.arn": []string{mockStsAssumeRolePolicyArn},
						"RoleArn":                 []string{mockStsAssumeRoleArn},
						"RoleSessionName":         []string{mockStsAssumeRoleSessionName},
						"Version":                 []string{"2011-06-15"},
					}.Encode()},
					Response: &awsbase.MockResponse{StatusCode: 200, Body: mockStsAssumeRoleValidResponse, ContentType: "text/xml"},
				},
				{
					Request:  &awsbase.MockRequest{Method: "POST", Uri: "/", Body: mockStsGetCallerIdentityRequestBody},
					Response: &awsbase.MockResponse{StatusCode: 200, Body: mockStsGetCallerIdentityValidResponseBody, ContentType: "text/xml"},
				},
			},
		},
		{
			Config: map[string]interface{}{
				"assume_role_tags": map[string]interface{}{
					mockStsAssumeRoleTagKey: mockStsAssumeRoleTagValue,
				},
				"bucket":       "tf-test",
				"key":          "state",
				"region":       "us-west-1",
				"role_arn":     mockStsAssumeRoleArn,
				"session_name": mockStsAssumeRoleSessionName,
			},
			Description: "assume_role_tags",
			MockStsEndpoints: []*awsbase.MockEndpoint{
				{
					Request: &awsbase.MockRequest{Method: "POST", Uri: "/", Body: url.Values{
						"Action":              []string{"AssumeRole"},
						"DurationSeconds":     []string{"900"},
						"RoleArn":             []string{mockStsAssumeRoleArn},
						"RoleSessionName":     []string{mockStsAssumeRoleSessionName},
						"Tags.member.1.Key":   []string{mockStsAssumeRoleTagKey},
						"Tags.member.1.Value": []string{mockStsAssumeRoleTagValue},
						"Version":             []string{"2011-06-15"},
					}.Encode()},
					Response: &awsbase.MockResponse{StatusCode: 200, Body: mockStsAssumeRoleValidResponse, ContentType: "text/xml"},
				},
				{
					Request:  &awsbase.MockRequest{Method: "POST", Uri: "/", Body: mockStsGetCallerIdentityRequestBody},
					Response: &awsbase.MockResponse{StatusCode: 200, Body: mockStsGetCallerIdentityValidResponseBody, ContentType: "text/xml"},
				},
			},
		},
		{
			Config: map[string]interface{}{
				"assume_role_tags": map[string]interface{}{
					mockStsAssumeRoleTagKey: mockStsAssumeRoleTagValue,
				},
				"assume_role_transitive_tag_keys": []interface{}{mockStsAssumeRoleTagKey},
				"bucket":                          "tf-test",
				"key":                             "state",
				"region":                          "us-west-1",
				"role_arn":                        mockStsAssumeRoleArn,
				"session_name":                    mockStsAssumeRoleSessionName,
			},
			Description: "assume_role_transitive_tag_keys",
			MockStsEndpoints: []*awsbase.MockEndpoint{
				{
					Request: &awsbase.MockRequest{Method: "POST", Uri: "/", Body: url.Values{
						"Action":                     []string{"AssumeRole"},
						"DurationSeconds":            []string{"900"},
						"RoleArn":                    []string{mockStsAssumeRoleArn},
						"RoleSessionName":            []string{mockStsAssumeRoleSessionName},
						"Tags.member.1.Key":          []string{mockStsAssumeRoleTagKey},
						"Tags.member.1.Value":        []string{mockStsAssumeRoleTagValue},
						"TransitiveTagKeys.member.1": []string{mockStsAssumeRoleTagKey},
						"Version":                    []string{"2011-06-15"},
					}.Encode()},
					Response: &awsbase.MockResponse{StatusCode: 200, Body: mockStsAssumeRoleValidResponse, ContentType: "text/xml"},
				},
				{
					Request:  &awsbase.MockRequest{Method: "POST", Uri: "/", Body: mockStsGetCallerIdentityRequestBody},
					Response: &awsbase.MockResponse{StatusCode: 200, Body: mockStsGetCallerIdentityValidResponseBody, ContentType: "text/xml"},
				},
			},
		},
	}

	for _, testCase := range testCases {
		testCase := testCase

		t.Run(testCase.Description, func(t *testing.T) {
			closeSts, mockStsSession, err := awsbase.GetMockedAwsApiSession("STS", testCase.MockStsEndpoints)
			defer closeSts()

			if err != nil {
				t.Fatalf("unexpected error creating mock STS server: %s", err)
			}

			if mockStsSession != nil && mockStsSession.Config != nil {
				testCase.Config["sts_endpoint"] = aws.StringValue(mockStsSession.Config.Endpoint)
			}

			diags := New().Configure(hcl2shim.HCL2ValueFromConfigValue(testCase.Config))

			if diags.HasErrors() {
				for _, diag := range diags {
					t.Errorf("unexpected error: %s", diag.Description().Summary)
				}
			}
		})
	}
}

func TestBackendConfig_invalidKey(t *testing.T) {
	testACC(t)
	cfg := hcl2shim.HCL2ValueFromConfigValue(map[string]interface{}{
		"region":         "us-west-1",
		"bucket":         "tf-test",
		"key":            "/leading-slash",
		"encrypt":        true,
		"dynamodb_table": "dynamoTable",
	})

	_, diags := New().PrepareConfig(cfg)
	if !diags.HasErrors() {
		t.Fatal("expected config validation error")
	}
}

func TestBackendConfig_invalidSSECustomerKeyLength(t *testing.T) {
	testACC(t)
	cfg := hcl2shim.HCL2ValueFromConfigValue(map[string]interface{}{
		"region":           "us-west-1",
		"bucket":           "tf-test",
		"encrypt":          true,
		"key":              "state",
		"dynamodb_table":   "dynamoTable",
		"sse_customer_key": "key",
	})

	_, diags := New().PrepareConfig(cfg)
	if !diags.HasErrors() {
		t.Fatal("expected error for invalid sse_customer_key length")
	}
}

func TestBackendConfig_invalidSSECustomerKeyEncoding(t *testing.T) {
	testACC(t)
	cfg := hcl2shim.HCL2ValueFromConfigValue(map[string]interface{}{
		"region":           "us-west-1",
		"bucket":           "tf-test",
		"encrypt":          true,
		"key":              "state",
		"dynamodb_table":   "dynamoTable",
		"sse_customer_key": "====CT70aTYB2JGff7AjQtwbiLkwH4npICay1PWtmdka",
	})

	diags := New().Configure(cfg)
	if !diags.HasErrors() {
		t.Fatal("expected error for failing to decode sse_customer_key")
	}
}

func TestBackendConfig_conflictingEncryptionSchema(t *testing.T) {
	testACC(t)
	cfg := hcl2shim.HCL2ValueFromConfigValue(map[string]interface{}{
		"region":           "us-west-1",
		"bucket":           "tf-test",
		"key":              "state",
		"encrypt":          true,
		"dynamodb_table":   "dynamoTable",
		"sse_customer_key": "1hwbcNPGWL+AwDiyGmRidTWAEVmCWMKbEHA+Es8w75o=",
		"kms_key_id":       "arn:aws:kms:us-west-2:111122223333:key/1234abcd-12ab-34cd-56ef-1234567890ab",
	})

	diags := New().Configure(cfg)
	if !diags.HasErrors() {
		t.Fatal("expected error for simultaneous usage of kms_key_id and sse_customer_key")
	}
}

func TestBackend(t *testing.T) {
	testACC(t)

	bucketName := fmt.Sprintf("terraform-remote-s3-test-%x", time.Now().Unix())
	keyName := "testState"

	b := backend.TestBackendConfig(t, New(), backend.TestWrapConfig(map[string]interface{}{
		"bucket":  bucketName,
		"key":     keyName,
		"encrypt": true,
	})).(*Backend)

	createS3Bucket(t, b.s3Client, bucketName)
	defer deleteS3Bucket(t, b.s3Client, bucketName)

	backend.TestBackendStates(t, b)
}

func TestBackendLocked(t *testing.T) {
	testACC(t)

	bucketName := fmt.Sprintf("terraform-remote-s3-test-%x", time.Now().Unix())
	keyName := "test/state"

	b1 := backend.TestBackendConfig(t, New(), backend.TestWrapConfig(map[string]interface{}{
		"bucket":         bucketName,
		"key":            keyName,
		"encrypt":        true,
		"dynamodb_table": bucketName,
	})).(*Backend)

	b2 := backend.TestBackendConfig(t, New(), backend.TestWrapConfig(map[string]interface{}{
		"bucket":         bucketName,
		"key":            keyName,
		"encrypt":        true,
		"dynamodb_table": bucketName,
	})).(*Backend)

	createS3Bucket(t, b1.s3Client, bucketName)
	defer deleteS3Bucket(t, b1.s3Client, bucketName)
	createDynamoDBTable(t, b1.dynClient, bucketName)
	defer deleteDynamoDBTable(t, b1.dynClient, bucketName)

	backend.TestBackendStateLocks(t, b1, b2)
	backend.TestBackendStateForceUnlock(t, b1, b2)
}

func TestBackendSSECustomerKey(t *testing.T) {
	testACC(t)
	bucketName := fmt.Sprintf("terraform-remote-s3-test-%x", time.Now().Unix())

	b := backend.TestBackendConfig(t, New(), backend.TestWrapConfig(map[string]interface{}{
		"bucket":           bucketName,
		"encrypt":          true,
		"key":              "test-SSE-C",
		"sse_customer_key": "4Dm1n4rphuFgawxuzY/bEfvLf6rYK0gIjfaDSLlfXNk=",
	})).(*Backend)

	createS3Bucket(t, b.s3Client, bucketName)
	defer deleteS3Bucket(t, b.s3Client, bucketName)

	backend.TestBackendStates(t, b)
}

// add some extra junk in S3 to try and confuse the env listing.
func TestBackendExtraPaths(t *testing.T) {
	testACC(t)
	bucketName := fmt.Sprintf("terraform-remote-s3-test-%x", time.Now().Unix())
	keyName := "test/state/tfstate"

	b := backend.TestBackendConfig(t, New(), backend.TestWrapConfig(map[string]interface{}{
		"bucket":  bucketName,
		"key":     keyName,
		"encrypt": true,
	})).(*Backend)

	createS3Bucket(t, b.s3Client, bucketName)
	defer deleteS3Bucket(t, b.s3Client, bucketName)

	// put multiple states in old env paths.
	s1 := states.NewState()
	s2 := states.NewState()

	// RemoteClient to Put things in various paths
	client := &RemoteClient{
		s3Client:             b.s3Client,
		dynClient:            b.dynClient,
		bucketName:           b.bucketName,
		path:                 b.path("s1"),
		serverSideEncryption: b.serverSideEncryption,
		acl:                  b.acl,
		kmsKeyID:             b.kmsKeyID,
		ddbTable:             b.ddbTable,
	}

	// Write the first state
	stateMgr := &remote.State{Client: client}
	stateMgr.WriteState(s1)
	if err := stateMgr.PersistState(); err != nil {
		t.Fatal(err)
	}

	// Write the second state
	// Note a new state manager - otherwise, because these
	// states are equal, the state will not Put to the remote
	client.path = b.path("s2")
	stateMgr2 := &remote.State{Client: client}
	stateMgr2.WriteState(s2)
	if err := stateMgr2.PersistState(); err != nil {
		t.Fatal(err)
	}

	s2Lineage := stateMgr2.StateSnapshotMeta().Lineage

	if err := checkStateList(b, []string{"default", "s1", "s2"}); err != nil {
		t.Fatal(err)
	}

	// put a state in an env directory name
	client.path = b.workspaceKeyPrefix + "/error"
	stateMgr.WriteState(states.NewState())
	if err := stateMgr.PersistState(); err != nil {
		t.Fatal(err)
	}
	if err := checkStateList(b, []string{"default", "s1", "s2"}); err != nil {
		t.Fatal(err)
	}

	// add state with the wrong key for an existing env
	client.path = b.workspaceKeyPrefix + "/s2/notTestState"
	stateMgr.WriteState(states.NewState())
	if err := stateMgr.PersistState(); err != nil {
		t.Fatal(err)
	}
	if err := checkStateList(b, []string{"default", "s1", "s2"}); err != nil {
		t.Fatal(err)
	}

	// remove the state with extra subkey
	if err := client.Delete(); err != nil {
		t.Fatal(err)
	}

	// delete the real workspace
	if err := b.DeleteWorkspace("s2"); err != nil {
		t.Fatal(err)
	}

	if err := checkStateList(b, []string{"default", "s1"}); err != nil {
		t.Fatal(err)
	}

	// fetch that state again, which should produce a new lineage
	s2Mgr, err := b.StateMgr("s2")
	if err != nil {
		t.Fatal(err)
	}
	if err := s2Mgr.RefreshState(); err != nil {
		t.Fatal(err)
	}

	if s2Mgr.(*remote.State).StateSnapshotMeta().Lineage == s2Lineage {
		t.Fatal("state s2 was not deleted")
	}
	s2 = s2Mgr.State()
	s2Lineage = stateMgr.StateSnapshotMeta().Lineage

	// add a state with a key that matches an existing environment dir name
	client.path = b.workspaceKeyPrefix + "/s2/"
	stateMgr.WriteState(states.NewState())
	if err := stateMgr.PersistState(); err != nil {
		t.Fatal(err)
	}

	// make sure s2 is OK
	s2Mgr, err = b.StateMgr("s2")
	if err != nil {
		t.Fatal(err)
	}
	if err := s2Mgr.RefreshState(); err != nil {
		t.Fatal(err)
	}

	if stateMgr.StateSnapshotMeta().Lineage != s2Lineage {
		t.Fatal("we got the wrong state for s2")
	}

	if err := checkStateList(b, []string{"default", "s1", "s2"}); err != nil {
		t.Fatal(err)
	}
}

// ensure we can separate the workspace prefix when it also matches the prefix
// of the workspace name itself.
func TestBackendPrefixInWorkspace(t *testing.T) {
	testACC(t)
	bucketName := fmt.Sprintf("terraform-remote-s3-test-%x", time.Now().Unix())

	b := backend.TestBackendConfig(t, New(), backend.TestWrapConfig(map[string]interface{}{
		"bucket":               bucketName,
		"key":                  "test-env.tfstate",
		"workspace_key_prefix": "env",
	})).(*Backend)

	createS3Bucket(t, b.s3Client, bucketName)
	defer deleteS3Bucket(t, b.s3Client, bucketName)

	// get a state that contains the prefix as a substring
	sMgr, err := b.StateMgr("env-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := sMgr.RefreshState(); err != nil {
		t.Fatal(err)
	}

	if err := checkStateList(b, []string{"default", "env-1"}); err != nil {
		t.Fatal(err)
	}
}

func TestKeyEnv(t *testing.T) {
	testACC(t)
	keyName := "some/paths/tfstate"

	bucket0Name := fmt.Sprintf("terraform-remote-s3-test-%x-0", time.Now().Unix())
	b0 := backend.TestBackendConfig(t, New(), backend.TestWrapConfig(map[string]interface{}{
		"bucket":               bucket0Name,
		"key":                  keyName,
		"encrypt":              true,
		"workspace_key_prefix": "",
	})).(*Backend)

	createS3Bucket(t, b0.s3Client, bucket0Name)
	defer deleteS3Bucket(t, b0.s3Client, bucket0Name)

	bucket1Name := fmt.Sprintf("terraform-remote-s3-test-%x-1", time.Now().Unix())
	b1 := backend.TestBackendConfig(t, New(), backend.TestWrapConfig(map[string]interface{}{
		"bucket":               bucket1Name,
		"key":                  keyName,
		"encrypt":              true,
		"workspace_key_prefix": "project/env:",
	})).(*Backend)

	createS3Bucket(t, b1.s3Client, bucket1Name)
	defer deleteS3Bucket(t, b1.s3Client, bucket1Name)

	bucket2Name := fmt.Sprintf("terraform-remote-s3-test-%x-2", time.Now().Unix())
	b2 := backend.TestBackendConfig(t, New(), backend.TestWrapConfig(map[string]interface{}{
		"bucket":  bucket2Name,
		"key":     keyName,
		"encrypt": true,
	})).(*Backend)

	createS3Bucket(t, b2.s3Client, bucket2Name)
	defer deleteS3Bucket(t, b2.s3Client, bucket2Name)

	if err := testGetWorkspaceForKey(b0, "some/paths/tfstate", ""); err != nil {
		t.Fatal(err)
	}

	if err := testGetWorkspaceForKey(b0, "ws1/some/paths/tfstate", "ws1"); err != nil {
		t.Fatal(err)
	}

	if err := testGetWorkspaceForKey(b1, "project/env:/ws1/some/paths/tfstate", "ws1"); err != nil {
		t.Fatal(err)
	}

	if err := testGetWorkspaceForKey(b1, "project/env:/ws2/some/paths/tfstate", "ws2"); err != nil {
		t.Fatal(err)
	}

	if err := testGetWorkspaceForKey(b2, "env:/ws3/some/paths/tfstate", "ws3"); err != nil {
		t.Fatal(err)
	}

	backend.TestBackendStates(t, b0)
	backend.TestBackendStates(t, b1)
	backend.TestBackendStates(t, b2)
}

func testGetWorkspaceForKey(b *Backend, key string, expected string) error {
	if actual := b.keyEnv(key); actual != expected {
		return fmt.Errorf("incorrect workspace for key[%q]. Expected[%q]: Actual[%q]", key, expected, actual)
	}
	return nil
}

func checkStateList(b backend.Backend, expected []string) error {
	states, err := b.Workspaces()
	if err != nil {
		return err
	}

	if !reflect.DeepEqual(states, expected) {
		return fmt.Errorf("incorrect states listed: %q", states)
	}
	return nil
}

func createS3Bucket(t *testing.T, s3Client *s3.S3, bucketName string) {
	createBucketReq := &s3.CreateBucketInput{
		Bucket: &bucketName,
	}

	// Be clear about what we're doing in case the user needs to clean
	// this up later.
	t.Logf("creating S3 bucket %s in %s", bucketName, *s3Client.Config.Region)
	_, err := s3Client.CreateBucket(createBucketReq)
	if err != nil {
		t.Fatal("failed to create test S3 bucket:", err)
	}
}

func deleteS3Bucket(t *testing.T, s3Client *s3.S3, bucketName string) {
	warning := "WARNING: Failed to delete the test S3 bucket. It may have been left in your AWS account and may incur storage charges. (error was %s)"

	// first we have to get rid of the env objects, or we can't delete the bucket
	resp, err := s3Client.ListObjects(&s3.ListObjectsInput{Bucket: &bucketName})
	if err != nil {
		t.Logf(warning, err)
		return
	}
	for _, obj := range resp.Contents {
		if _, err := s3Client.DeleteObject(&s3.DeleteObjectInput{Bucket: &bucketName, Key: obj.Key}); err != nil {
			// this will need cleanup no matter what, so just warn and exit
			t.Logf(warning, err)
			return
		}
	}

	if _, err := s3Client.DeleteBucket(&s3.DeleteBucketInput{Bucket: &bucketName}); err != nil {
		t.Logf(warning, err)
	}
}

// create the dynamoDB table, and wait until we can query it.
func createDynamoDBTable(t *testing.T, dynClient *dynamodb.DynamoDB, tableName string) {
	createInput := &dynamodb.CreateTableInput{
		AttributeDefinitions: []*dynamodb.AttributeDefinition{
			{
				AttributeName: aws.String("LockID"),
				AttributeType: aws.String("S"),
			},
		},
		KeySchema: []*dynamodb.KeySchemaElement{
			{
				AttributeName: aws.String("LockID"),
				KeyType:       aws.String("HASH"),
			},
		},
		ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
			ReadCapacityUnits:  aws.Int64(5),
			WriteCapacityUnits: aws.Int64(5),
		},
		TableName: aws.String(tableName),
	}

	_, err := dynClient.CreateTable(createInput)
	if err != nil {
		t.Fatal(err)
	}

	// now wait until it's ACTIVE
	start := time.Now()
	time.Sleep(time.Second)

	describeInput := &dynamodb.DescribeTableInput{
		TableName: aws.String(tableName),
	}

	for {
		resp, err := dynClient.DescribeTable(describeInput)
		if err != nil {
			t.Fatal(err)
		}

		if *resp.Table.TableStatus == "ACTIVE" {
			return
		}

		if time.Since(start) > time.Minute {
			t.Fatalf("timed out creating DynamoDB table %s", tableName)
		}

		time.Sleep(3 * time.Second)
	}

}

func deleteDynamoDBTable(t *testing.T, dynClient *dynamodb.DynamoDB, tableName string) {
	params := &dynamodb.DeleteTableInput{
		TableName: aws.String(tableName),
	}
	_, err := dynClient.DeleteTable(params)
	if err != nil {
		t.Logf("WARNING: Failed to delete the test DynamoDB table %q. It has been left in your AWS account and may incur charges. (error was %s)", tableName, err)
	}
}
package jsonrpc3

import (
	"encoding/json"
	"testing"
)

func TestRequest_Marshaling(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		want string
	}{
		{
			name: "basic request",
			req: Request{
				JSONRPC: Version30,
				Method:  "test",
				ID:      1,
			},
			want: `{"jsonrpc":"3.0","method":"test","id":1}`,
		},
		{
			name: "request with params",
			req: Request{
				JSONRPC: Version30,
				Method:  "add",
				Params:  RawMessage(`[1,2]`),
				ID:      "test-id",
			},
			want: `{"jsonrpc":"3.0","method":"add","params":[1,2],"id":"test-id"}`,
		},
		{
			name: "request with ref",
			req: Request{
				JSONRPC: Version30,
				Ref:     "db-1",
				Method:  "query",
				Params:  RawMessage(`["SELECT * FROM users"]`),
				ID:      2,
			},
			want: `{"jsonrpc":"3.0","ref":"db-1","method":"query","params":["SELECT * FROM users"],"id":2}`,
		},
		{
			name: "notification (no ID)",
			req: Request{
				JSONRPC: Version30,
				Method:  "notify",
				Params:  RawMessage(`{"event":"update"}`),
			},
			want: `{"jsonrpc":"3.0","method":"notify","params":{"event":"update"}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.req)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Marshal() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestRequest_Unmarshaling(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    Request
		wantErr bool
	}{
		{
			name:  "basic request",
			input: `{"jsonrpc":"3.0","method":"test","id":1}`,
			want: Request{
				JSONRPC: Version30,
				Method:  "test",
				ID:      float64(1), // JSON numbers unmarshal to float64
			},
		},
		{
			name:  "request with ref",
			input: `{"jsonrpc":"3.0","ref":"db-1","method":"query","id":2}`,
			want: Request{
				JSONRPC: Version30,
				Ref:     "db-1",
				Method:  "query",
				ID:      float64(2),
			},
		},
		{
			name:  "request with string ID",
			input: `{"jsonrpc":"3.0","method":"test","id":"abc"}`,
			want: Request{
				JSONRPC: Version30,
				Method:  "test",
				ID:      "abc",
			},
		},
		{
			name:  "notification",
			input: `{"jsonrpc":"3.0","method":"notify"}`,
			want: Request{
				JSONRPC: Version30,
				Method:  "notify",
				ID:      nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Request
			err := json.Unmarshal([]byte(tt.input), &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Unmarshal() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil {
				if got.JSONRPC != tt.want.JSONRPC {
					t.Errorf("JSONRPC = %v, want %v", got.JSONRPC, tt.want.JSONRPC)
				}
				if got.Method != tt.want.Method {
					t.Errorf("Method = %v, want %v", got.Method, tt.want.Method)
				}
				if got.Ref != tt.want.Ref {
					t.Errorf("Ref = %v, want %v", got.Ref, tt.want.Ref)
				}
			}
		})
	}
}

func TestRequest_IsNotification(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		want bool
	}{
		{
			name: "request with ID",
			req:  Request{ID: 1},
			want: false,
		},
		{
			name: "notification (nil ID)",
			req:  Request{ID: nil},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.req.IsNotification(); got != tt.want {
				t.Errorf("IsNotification() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResponse_Marshaling(t *testing.T) {
	tests := []struct {
		name string
		resp Response
		want string
	}{
		{
			name: "success response",
			resp: Response{
				JSONRPC: Version30,
				Result:  RawMessage(`"ok"`),
				ID:      1,
			},
			want: `{"jsonrpc":"3.0","result":"ok","id":1}`,
		},
		{
			name: "error response",
			resp: Response{
				JSONRPC: Version30,
				Error: &Error{
					Code:    CodeMethodNotFound,
					Message: "Method not found",
				},
				ID: 1,
			},
			want: `{"jsonrpc":"3.0","error":{"code":-32601,"message":"Method not found"},"id":1}`,
		},
		{
			name: "error with data",
			resp: Response{
				JSONRPC: Version30,
				Error: &Error{
					Code:    CodeInvalidParams,
					Message: "Invalid params",
					Data:    "missing required field",
				},
				ID: "req-1",
			},
			want: `{"jsonrpc":"3.0","error":{"code":-32602,"message":"Invalid params","data":"missing required field"},"id":"req-1"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.resp)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Marshal() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  Error
		want string
	}{
		{
			name: "error without data",
			err:  Error{Code: -32601, Message: "Method not found"},
			want: "jsonrpc error -32601: Method not found",
		},
		{
			name: "error with data",
			err:  Error{Code: -32602, Message: "Invalid params", Data: "missing field"},
			want: "jsonrpc error -32602: Invalid params (data: missing field)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestErrorConstructors(t *testing.T) {
	tests := []struct {
		name string
		fn   func() *Error
		code int
		msg  string
	}{
		{
			name: "ParseError",
			fn:   func() *Error { return NewParseError("invalid json") },
			code: CodeParseError,
			msg:  "Parse error",
		},
		{
			name: "InvalidRequestError",
			fn:   func() *Error { return NewInvalidRequestError("missing method") },
			code: CodeInvalidRequest,
			msg:  "Invalid Request",
		},
		{
			name: "MethodNotFoundError",
			fn:   func() *Error { return NewMethodNotFoundError("test") },
			code: CodeMethodNotFound,
			msg:  "Method not found",
		},
		{
			name: "InvalidParamsError",
			fn:   func() *Error { return NewInvalidParamsError("wrong type") },
			code: CodeInvalidParams,
			msg:  "Invalid params",
		},
		{
			name: "InternalError",
			fn:   func() *Error { return NewInternalError("panic") },
			code: CodeInternalError,
			msg:  "Internal error",
		},
		{
			name: "InvalidReferenceError",
			fn:   func() *Error { return NewInvalidReferenceError("empty ref") },
			code: CodeInvalidReference,
			msg:  "Invalid reference",
		},
		{
			name: "ReferenceNotFoundError",
			fn:   func() *Error { return NewReferenceNotFoundError("db-1") },
			code: CodeReferenceNotFound,
			msg:  "Reference not found",
		},
		{
			name: "ReferenceTypeError",
			fn:   func() *Error { return NewReferenceTypeError("wrong type") },
			code: CodeReferenceTypeError,
			msg:  "Reference type error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.fn()
			if err.Code != tt.code {
				t.Errorf("Code = %v, want %v", err.Code, tt.code)
			}
			if err.Message != tt.msg {
				t.Errorf("Message = %v, want %v", err.Message, tt.msg)
			}
		})
	}
}

func TestReference_Marshaling(t *testing.T) {
	tests := []struct {
		name string
		ref  Reference
		want string
	}{
		{
			name: "simple reference",
			ref:  Reference{Ref: "obj-1"},
			want: `{"$ref":"obj-1"}`,
		},
		{
			name: "uuid reference",
			ref:  Reference{Ref: "550e8400-e29b-41d4-a716-446655440000"},
			want: `{"$ref":"550e8400-e29b-41d4-a716-446655440000"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.ref)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("Marshal() = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestReference_Unmarshaling(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  Reference
	}{
		{
			name:  "simple reference",
			input: `{"$ref":"obj-1"}`,
			want:  Reference{Ref: "obj-1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got Reference
			err := json.Unmarshal([]byte(tt.input), &got)
			if err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if got.Ref != tt.want.Ref {
				t.Errorf("Ref = %v, want %v", got.Ref, tt.want.Ref)
			}
		})
	}
}

func TestNewRequest(t *testing.T) {
	req, err := NewRequest("test", map[string]string{"key": "value"}, 1)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	if req.JSONRPC != Version30 {
		t.Errorf("JSONRPC = %v, want %v", req.JSONRPC, Version30)
	}
	if req.Method != "test" {
		t.Errorf("Method = %v, want %v", req.Method, "test")
	}
	if req.ID != 1 {
		t.Errorf("ID = %v, want %v", req.ID, 1)
	}
	if req.Params == nil {
		t.Error("Params should not be nil")
	}
}

func TestNewRequestWithRef(t *testing.T) {
	req, err := NewRequestWithRef("db-1", "query", []string{"SELECT 1"}, 1)
	if err != nil {
		t.Fatalf("NewRequestWithRef() error = %v", err)
	}
	if req.Ref != "db-1" {
		t.Errorf("Ref = %v, want %v", req.Ref, "db-1")
	}
	if req.Method != "query" {
		t.Errorf("Method = %v, want %v", req.Method, "query")
	}
}

func TestNewNotification(t *testing.T) {
	req, err := NewNotification("notify", nil)
	if err != nil {
		t.Fatalf("NewNotification() error = %v", err)
	}
	if req.ID != nil {
		t.Errorf("ID should be nil for notification, got %v", req.ID)
	}
	if !req.IsNotification() {
		t.Error("IsNotification() should return true")
	}
}

func TestNewSuccessResponse(t *testing.T) {
	resp, err := NewSuccessResponse(1, "ok", Version30)
	if err != nil {
		t.Fatalf("NewSuccessResponse() error = %v", err)
	}
	if resp.JSONRPC != Version30 {
		t.Errorf("JSONRPC = %v, want %v", resp.JSONRPC, Version30)
	}
	if resp.ID != 1 {
		t.Errorf("ID = %v, want %v", resp.ID, 1)
	}
	if resp.Error != nil {
		t.Error("Error should be nil for success response")
	}
	if resp.Result == nil {
		t.Error("Result should not be nil")
	}
}

func TestNewErrorResponse(t *testing.T) {
	resp := NewErrorResponse(1, NewMethodNotFoundError("test"), Version30)
	if resp.JSONRPC != Version30 {
		t.Errorf("JSONRPC = %v, want %v", resp.JSONRPC, Version30)
	}
	if resp.ID != 1 {
		t.Errorf("ID = %v, want %v", resp.ID, 1)
	}
	if resp.Result != nil {
		t.Error("Result should be nil for error response")
	}
	if resp.Error == nil {
		t.Fatal("Error should not be nil")
	}
	if resp.Error.Code != CodeMethodNotFound {
		t.Errorf("Error code = %v, want %v", resp.Error.Code, CodeMethodNotFound)
	}
}

func TestMessage_IsRequest(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want bool
	}{
		{
			name: "request with method",
			msg: Message{
				JSONRPC: Version30,
				Method:  "test",
				ID:      1,
			},
			want: true,
		},
		{
			name: "response with result",
			msg: Message{
				JSONRPC: Version30,
				Result:  RawMessage(`"ok"`),
				ID:      1,
			},
			want: false,
		},
		{
			name: "response with error",
			msg: Message{
				JSONRPC: Version30,
				Error:   &Error{Code: CodeInternalError, Message: "test"},
				ID:      1,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.IsRequest(); got != tt.want {
				t.Errorf("IsRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessage_IsResponse(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want bool
	}{
		{
			name: "request",
			msg: Message{
				JSONRPC: Version30,
				Method:  "test",
				ID:      1,
			},
			want: false,
		},
		{
			name: "response with result",
			msg: Message{
				JSONRPC: Version30,
				Result:  RawMessage(`"ok"`),
				ID:      1,
			},
			want: true,
		},
		{
			name: "response with error",
			msg: Message{
				JSONRPC: Version30,
				Error:   &Error{Code: CodeInternalError, Message: "test"},
				ID:      1,
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.IsResponse(); got != tt.want {
				t.Errorf("IsResponse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessage_IsNotification(t *testing.T) {
	tests := []struct {
		name string
		msg  Message
		want bool
	}{
		{
			name: "request with ID",
			msg: Message{
				JSONRPC: Version30,
				Method:  "test",
				ID:      1,
			},
			want: false,
		},
		{
			name: "notification (no ID)",
			msg: Message{
				JSONRPC: Version30,
				Method:  "test",
			},
			want: true,
		},
		{
			name: "response",
			msg: Message{
				JSONRPC: Version30,
				Result:  RawMessage(`"ok"`),
				ID:      1,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.IsNotification(); got != tt.want {
				t.Errorf("IsNotification() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMessage_ToRequest(t *testing.T) {
	tests := []struct {
		name    string
		msg     Message
		wantNil bool
	}{
		{
			name: "valid request",
			msg: Message{
				JSONRPC: Version30,
				Method:  "test",
				Params:  RawMessage(`[1,2]`),
				ID:      1,
			},
			wantNil: false,
		},
		{
			name: "response returns nil",
			msg: Message{
				JSONRPC: Version30,
				Result:  RawMessage(`"ok"`),
				ID:      1,
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := tt.msg.ToRequest()
			if tt.wantNil {
				if req != nil {
					t.Errorf("ToRequest() = %v, want nil", req)
				}
			} else {
				if req == nil {
					t.Fatal("ToRequest() returned nil")
				}
				if req.Method != tt.msg.Method {
					t.Errorf("Method = %v, want %v", req.Method, tt.msg.Method)
				}
				if req.ID != tt.msg.ID {
					t.Errorf("ID = %v, want %v", req.ID, tt.msg.ID)
				}
			}
		})
	}
}

func TestMessage_ToResponse(t *testing.T) {
	tests := []struct {
		name    string
		msg     Message
		wantNil bool
	}{
		{
			name: "valid response with result",
			msg: Message{
				JSONRPC: Version30,
				Result:  RawMessage(`"ok"`),
				ID:      1,
			},
			wantNil: false,
		},
		{
			name: "valid response with error",
			msg: Message{
				JSONRPC: Version30,
				Error:   &Error{Code: CodeInternalError, Message: "test"},
				ID:      1,
			},
			wantNil: false,
		},
		{
			name: "request returns nil",
			msg: Message{
				JSONRPC: Version30,
				Method:  "test",
				ID:      1,
			},
			wantNil: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := tt.msg.ToResponse()
			if tt.wantNil {
				if resp != nil {
					t.Errorf("ToResponse() = %v, want nil", resp)
				}
			} else {
				if resp == nil {
					t.Fatal("ToResponse() returned nil")
				}
				if resp.ID != tt.msg.ID {
					t.Errorf("ID = %v, want %v", resp.ID, tt.msg.ID)
				}
			}
		})
	}
}

func TestMessage_Unmarshal(t *testing.T) {
	tests := []struct {
		name       string
		json       string
		wantMethod string
		wantResult bool
		wantError  bool
	}{
		{
			name:       "request message",
			json:       `{"jsonrpc":"3.0","method":"test","id":1}`,
			wantMethod: "test",
			wantResult: false,
			wantError:  false,
		},
		{
			name:       "response message with result",
			json:       `{"jsonrpc":"3.0","result":"ok","id":1}`,
			wantMethod: "",
			wantResult: true,
			wantError:  false,
		},
		{
			name:       "response message with error",
			json:       `{"jsonrpc":"3.0","error":{"code":-32603,"message":"test"},"id":1}`,
			wantMethod: "",
			wantResult: false,
			wantError:  true,
		},
		{
			name:       "notification",
			json:       `{"jsonrpc":"3.0","method":"notify"}`,
			wantMethod: "notify",
			wantResult: false,
			wantError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var msg Message
			if err := json.Unmarshal([]byte(tt.json), &msg); err != nil {
				t.Fatalf("Unmarshal failed: %v", err)
			}

			if msg.Method != tt.wantMethod {
				t.Errorf("Method = %v, want %v", msg.Method, tt.wantMethod)
			}

			hasResult := msg.Result != nil
			if hasResult != tt.wantResult {
				t.Errorf("has Result = %v, want %v", hasResult, tt.wantResult)
			}

			hasError := msg.Error != nil
			if hasError != tt.wantError {
				t.Errorf("has Error = %v, want %v", hasError, tt.wantError)
			}

			// Verify conversion works
			if msg.IsRequest() {
				req := msg.ToRequest()
				if req == nil {
					t.Error("ToRequest() returned nil for request message")
				}
			}

			if msg.IsResponse() {
				resp := msg.ToResponse()
				if resp == nil {
					t.Error("ToResponse() returned nil for response message")
				}
			}
		})
	}
}

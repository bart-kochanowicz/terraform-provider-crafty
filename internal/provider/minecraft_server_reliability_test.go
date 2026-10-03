package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/bart-kochanowicz/terraform-provider-crafty/internal/client"
)

func testTimeouts(values map[string]string) timeouts.Value {
	attrs := map[string]attr.Type{"create": types.StringType, "read": types.StringType, "update": types.StringType, "delete": types.StringType}
	if values == nil {
		return timeouts.Value{Object: types.ObjectNull(attrs)}
	}
	data := map[string]attr.Value{}
	for key := range attrs {
		data[key] = types.StringNull()
	}
	for key, value := range values {
		data[key] = types.StringValue(value)
	}
	return timeouts.Value{Object: types.ObjectValueMust(attrs, data)}
}

func reliabilityModel() serverModel {
	return serverModel{ID: types.StringValue("abc"), Name: types.StringValue("original"), Engine: types.StringValue("paper"), Version: types.StringValue("1.21.1"), MemMin: types.Int64Value(1), MemMax: types.Int64Value(2), Host: types.StringValue("127.0.0.1"), Port: types.Int64Value(25565), AutoStart: types.BoolValue(false), Timeouts: testTimeouts(map[string]string{"create": "30ms", "read": "30ms", "update": "30ms", "delete": "30ms"})}
}

func reliabilityState(t *testing.T, s *serverResource, m serverModel) tfsdk.State {
	t.Helper()
	var schema resource.SchemaResponse
	s.Schema(context.Background(), resource.SchemaRequest{}, &schema)
	state := tfsdk.State{Schema: schema.Schema}
	if d := state.Set(context.Background(), &m); d.HasError() {
		t.Fatal(d)
	}
	return state
}

func reliabilityCreate(t *testing.T, s *serverResource, m serverModel) resource.CreateResponse {
	t.Helper()
	m.ID = types.StringUnknown()
	m.AutoStart = types.BoolUnknown()
	state := reliabilityState(t, s, m)
	response := resource.CreateResponse{State: tfsdk.State{Schema: state.Schema}}
	// Framework's private state type is internal. Initialize its zero value for
	// direct method tests; the RPC framework initializes it in production.
	field := reflect.ValueOf(&response.Private).Elem()
	field.Set(reflect.New(field.Type().Elem()))
	s.Create(context.Background(), resource.CreateRequest{Plan: tfsdk.Plan(state)}, &response)
	return response
}

func assertServerID(t *testing.T, state tfsdk.State) serverModel {
	t.Helper()
	var m serverModel
	if d := state.Get(context.Background(), &m); d.HasError() {
		t.Fatal(d)
	}
	if m.ID.ValueString() != "abc" {
		t.Fatal("created ID was lost")
	}
	return m
}

func writeServer(w http.ResponseWriter, name string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok", "data": []any{map[string]any{"server_id": "abc", "server_name": name, "auto_start": false, "server_ip": "127.0.0.1", "server_port": int64(25565), "execution_command": "java -Xms1000M -Xmx2000M -jar paper.jar nogui"}}})
}

func TestCreateRefreshFailureDoesNotTaint(t *testing.T) {
	var posts, gets atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			_, _ = w.Write([]byte(`{"status":"ok","data":{"new_server_id":"abc"}}`))
			return
		}
		gets.Add(1)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer api.Close()
	s := &serverResource{client: client.New(api.URL, "secret"), pollInterval: time.Millisecond}
	response := reliabilityCreate(t, s, reliabilityModel())
	assertServerID(t, response.State)
	if response.Diagnostics.HasError() || response.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("post-create read failure taints an existing server: %v", response.Diagnostics)
	}
	marker, d := response.Private.GetKey(context.Background(), pendingRefreshKey)
	if d.HasError() || string(marker) != "true" {
		t.Fatalf("preparation marker missing: %s %v", marker, d)
	}
	if posts.Load() != 1 || gets.Load() != 1 {
		t.Fatalf("permanent error retried: POST=%d GET=%d", posts.Load(), gets.Load())
	}
}

func TestCreateWaitsForPreparationAndTransientErrors(t *testing.T) {
	var posts, gets atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			posts.Add(1)
			_, _ = w.Write([]byte(`{"status":"ok","data":{"new_server_id":"abc"}}`))
			return
		}
		switch gets.Add(1) {
		case 1:
			_, _ = w.Write([]byte(`{"status":"ok","data":[]}`))
		case 2:
			w.WriteHeader(503)
		case 3:
			_, _ = w.Write([]byte(`{"status":"ok","data":[{"server_id":"abc"}]}`))
		case 4:
			writeServer(w, "preparing")
		default:
			writeServer(w, "original")
		}
	}))
	defer api.Close()
	s := &serverResource{client: client.New(api.URL, ""), pollInterval: time.Millisecond}
	m := reliabilityModel()
	m.Timeouts = testTimeouts(map[string]string{"create": "1s"})
	response := reliabilityCreate(t, s, m)
	actual := assertServerID(t, response.State)
	if response.Diagnostics.HasError() || response.Diagnostics.WarningsCount() != 0 || actual.Name.ValueString() != "original" || actual.AutoStart.IsNull() {
		t.Fatalf("preparation failed: %+v %v", actual, response.Diagnostics)
	}
	marker, _ := response.Private.GetKey(context.Background(), pendingRefreshKey)
	if len(marker) != 0 || posts.Load() != 1 || gets.Load() != 5 {
		t.Fatalf("unexpected calls or marker: %s POST=%d GET=%d", marker, posts.Load(), gets.Load())
	}
}

func TestPendingCreateSurvivesReadTimeoutAndRecovers(t *testing.T) {
	var ready atomic.Bool
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			_, _ = w.Write([]byte(`{"status":"ok","data":{"new_server_id":"abc"}}`))
			return
		}
		if ready.Load() {
			writeServer(w, "original")
		} else {
			_, _ = w.Write([]byte(`{"status":"ok","data":[]}`))
		}
	}))
	defer api.Close()
	s := &serverResource{client: client.New(api.URL, ""), pollInterval: time.Millisecond}
	created := reliabilityCreate(t, s, reliabilityModel())
	assertServerID(t, created.State)
	if created.Diagnostics.HasError() || created.Diagnostics.WarningsCount() != 1 {
		t.Fatal(created.Diagnostics)
	}
	read := resource.ReadResponse{State: created.State, Private: created.Private}
	s.Read(context.Background(), resource.ReadRequest{State: created.State, Private: created.Private}, &read)
	assertServerID(t, read.State)
	if read.Diagnostics.HasError() || read.Diagnostics.WarningsCount() != 1 {
		t.Fatal(read.Diagnostics)
	}
	ready.Store(true)
	recovered := resource.ReadResponse{State: read.State, Private: read.Private}
	s.Read(context.Background(), resource.ReadRequest{State: read.State, Private: read.Private}, &recovered)
	actual := assertServerID(t, recovered.State)
	marker, _ := recovered.Private.GetKey(context.Background(), pendingRefreshKey)
	if recovered.Diagnostics.HasError() || actual.AutoStart.IsNull() || len(marker) != 0 {
		t.Fatalf("failed to recover: %v marker=%s", recovered.Diagnostics, marker)
	}
}

func TestReadRetainsStateWhenVisibilityIsLost(t *testing.T) {
	for _, tc := range []struct {
		name      string
		visible   bool
		status    int
		wantError bool
		filtered  bool
	}{
		{"temporary absence", true, 0, false, false}, {"lost access with empty collection", false, 0, true, false},
		{"lost access with other visible servers", false, 0, true, true},
		{"transient unavailable", false, 503, true, false}, {"collection 404", false, 404, true, false}, {"unauthorized", false, 401, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gets atomic.Int32
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				n := gets.Add(1)
				if tc.status != 0 {
					w.WriteHeader(tc.status)
					return
				}
				if tc.visible && n >= 3 {
					writeServer(w, "original")
				} else if tc.filtered {
					_, _ = w.Write([]byte(`{"status":"ok","data":[{"server_id":"other"}]}`))
				} else {
					_, _ = w.Write([]byte(`{"status":"ok","data":[]}`))
				}
			}))
			defer api.Close()
			s := &serverResource{client: client.New(api.URL, ""), pollInterval: time.Millisecond}
			state := reliabilityState(t, s, reliabilityModel())
			response := resource.ReadResponse{State: state}
			s.Read(context.Background(), resource.ReadRequest{State: state}, &response)
			if response.Diagnostics.HasError() != tc.wantError {
				t.Fatal(response.Diagnostics)
			}
			assertServerID(t, response.State)
			if tc.wantError && !response.State.Raw.Equal(state.Raw) {
				t.Fatal("read changed state while visibility was unresolved")
			}
			if tc.status == 401 || tc.status == 404 {
				if gets.Load() != 1 {
					t.Fatal("permanent failure retried")
				}
			} else if gets.Load() < 3 {
				t.Fatal("read did not retry")
			}
		})
	}
}

func TestMutationTimeoutsKeepIdentityAndDoNotReplay(t *testing.T) {
	for _, operation := range []string{"POST", "PATCH", "DELETE"} {
		t.Run(operation, func(t *testing.T) {
			var mutations atomic.Int32
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == operation {
					mutations.Add(1)
					_, _ = io.Copy(io.Discard, r.Body)
					select {
					case <-r.Context().Done():
					case <-time.After(time.Second):
					}
					return
				}
				writeServer(w, "original")
			}))
			defer api.Close()
			s := &serverResource{client: client.New(api.URL, ""), pollInterval: time.Millisecond}
			state := reliabilityState(t, s, reliabilityModel())
			switch operation {
			case "POST":
				response := reliabilityCreate(t, s, reliabilityModel())
				if !response.Diagnostics.HasError() {
					t.Fatal("create timeout did not fail")
				}
			case "PATCH":
				response := resource.UpdateResponse{State: state}
				changed := reliabilityModel()
				changed.Name = types.StringValue("renamed")
				s.Update(context.Background(), resource.UpdateRequest{State: state, Plan: tfsdk.Plan(reliabilityState(t, s, changed))}, &response)
				if !response.Diagnostics.HasError() {
					t.Fatal("update timeout did not fail")
				}
				assertServerID(t, response.State)
			case "DELETE":
				response := resource.DeleteResponse{State: state}
				s.Delete(context.Background(), resource.DeleteRequest{State: state}, &response)
				if !response.Diagnostics.HasError() {
					t.Fatal("delete timeout did not fail")
				}
				assertServerID(t, response.State)
			}
			if mutations.Load() != 1 {
				t.Fatalf("mutation replayed: %d", mutations.Load())
			}
		})
	}
}

func TestUpdateRefreshFailurePreservesAppliedName(t *testing.T) {
	var patches atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "PATCH" {
			patches.Add(1)
			_, _ = w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.WriteHeader(503)
	}))
	defer api.Close()
	s := &serverResource{client: client.New(api.URL, ""), pollInterval: time.Millisecond}
	m := reliabilityModel()
	prior := reliabilityState(t, s, m)
	m.Name = types.StringValue("renamed")
	m.AutoStart = types.BoolUnknown()
	planState := reliabilityState(t, s, m)
	response := resource.UpdateResponse{State: prior}
	s.Update(context.Background(), resource.UpdateRequest{State: prior, Plan: tfsdk.Plan{Schema: prior.Schema, Raw: planState.Raw}}, &response)
	actual := assertServerID(t, response.State)
	if response.Diagnostics.HasError() || response.Diagnostics.WarningsCount() != 1 || actual.Name.ValueString() != "renamed" || actual.AutoStart.IsUnknown() || patches.Load() != 1 {
		t.Fatalf("accepted rename lost: %+v %v", actual, response.Diagnostics)
	}
}

func TestDeleteWaitsForRemovalAndPreservesIDOnFailure(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(map[bool]string{false: "asynchronous delete", true: "verification timeout"}[timeout], func(t *testing.T) {
			var deletes, gets atomic.Int32
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "DELETE" {
					deletes.Add(1)
					_, _ = w.Write([]byte(`{"status":"ok"}`))
					return
				}
				n := gets.Add(1)
				if timeout {
					w.WriteHeader(503)
				} else if n < 3 {
					_, _ = w.Write([]byte(`{"status":"ok","data":[{"server_id":"abc"}]}`))
				} else {
					_, _ = w.Write([]byte(`{"status":"ok","data":[]}`))
				}
			}))
			defer api.Close()
			s := &serverResource{client: client.New(api.URL, ""), pollInterval: time.Millisecond}
			model := reliabilityModel()
			if !timeout {
				model.Timeouts = testTimeouts(map[string]string{"delete": "1s"})
			}
			state := reliabilityState(t, s, model)
			response := resource.DeleteResponse{State: state}
			s.Delete(context.Background(), resource.DeleteRequest{State: state}, &response)
			if response.Diagnostics.HasError() != timeout || deletes.Load() != 1 || gets.Load() < 3 {
				t.Fatalf("delete verification failed: %v DELETE=%d GET=%d", response.Diagnostics, deletes.Load(), gets.Load())
			}
			if timeout {
				assertServerID(t, response.State)
			}
		})
	}
}

func TestReadRespectsRetryAfterAndCancellation(t *testing.T) {
	var gets atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		gets.Add(1)
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(429)
	}))
	defer api.Close()
	s := &serverResource{client: client.New(api.URL, ""), pollInterval: time.Millisecond}
	state := reliabilityState(t, s, reliabilityModel())
	response := resource.ReadResponse{State: state}
	s.Read(context.Background(), resource.ReadRequest{State: state}, &response)
	if !response.Diagnostics.HasError() || gets.Load() != 1 {
		t.Fatalf("Retry-After was ignored: GET=%d %v", gets.Load(), response.Diagnostics)
	}
	assertServerID(t, response.State)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	response = resource.ReadResponse{State: state}
	s.Read(ctx, resource.ReadRequest{State: state}, &response)
	if !response.Diagnostics.HasError() || gets.Load() != 1 {
		t.Fatal("cancelled read made another request")
	}
}

func TestRejectsInvalidTimeouts(t *testing.T) {
	for _, value := range []string{"0s", "-1s", "invalid"} {
		t.Run(value, func(t *testing.T) {
			s := &serverResource{}
			model := reliabilityModel()
			model.Timeouts = testTimeouts(map[string]string{"create": value})
			state := reliabilityState(t, s, model)
			response := resource.ValidateConfigResponse{}
			s.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(state)}, &response)
			if !response.Diagnostics.HasError() {
				t.Fatal("invalid duration accepted")
			}
		})
	}
}

func TestCreateDoesNotReplayAmbiguousResponse(t *testing.T) {
	var posts atomic.Int32
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("unexpected follow-up request: %s", r.Method)
			w.WriteHeader(500)
			return
		}
		posts.Add(1)
		// Crafty may have created the server before the response connection was lost.
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte(`{"status":"ok","data":{"new_server_id":`))
	}))
	defer api.Close()
	s := &serverResource{client: client.New(api.URL, ""), pollInterval: time.Millisecond}
	response := reliabilityCreate(t, s, reliabilityModel())
	if !response.Diagnostics.HasError() || posts.Load() != 1 {
		t.Fatalf("ambiguous POST was repeated: POST=%d %v", posts.Load(), response.Diagnostics)
	}
}

func TestServerNameContractValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		valid bool
	}{{"x", false}, {"bad/name", false}, {"bad\\name", false}, {"bad#name", false}, {"🚀", false}, {"🚀🚀", true}, {"valid-name", true}} {
		t.Run(tc.name, func(t *testing.T) {
			model := reliabilityModel()
			model.Name = types.StringValue(tc.name)
			if got := model.validate() == nil; got != tc.valid {
				t.Fatalf("name accepted=%v, want %v", got, tc.valid)
			}
			s := &serverResource{}
			state := reliabilityState(t, s, model)
			response := resource.ValidateConfigResponse{}
			s.ValidateConfig(context.Background(), resource.ValidateConfigRequest{Config: tfsdk.Config(state)}, &response)
			if response.Diagnostics.HasError() == tc.valid {
				t.Fatalf("configuration diagnostics disagree with API name constraint: %v", response.Diagnostics)
			}
		})
	}
}

func TestDeleteRequiresConsecutiveAbsences(t *testing.T) {
	for _, tc := range []struct {
		name     string
		sequence []int
	}{
		{"reappearing server", []int{0, 0, 1, 0, 0, 0}},
		{"transient error", []int{0, 0, 503, 0, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gets, deletes atomic.Int32
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "DELETE" {
					deletes.Add(1)
					_, _ = io.WriteString(w, `{"status":"ok"}`)
					return
				}
				index := int(gets.Add(1)) - 1
				if index >= len(tc.sequence) {
					t.Error("unexpected extra read")
					w.WriteHeader(500)
					return
				}
				switch tc.sequence[index] {
				case 0:
					_, _ = io.WriteString(w, `{"status":"ok","data":[]}`)
				case 1:
					_, _ = io.WriteString(w, `{"status":"ok","data":[{"server_id":"abc"}]}`)
				default:
					w.WriteHeader(tc.sequence[index])
				}
			}))
			defer api.Close()
			s := &serverResource{client: client.New(api.URL, ""), pollInterval: time.Millisecond}
			model := reliabilityModel()
			model.Timeouts = testTimeouts(map[string]string{"delete": "1s"})
			state := reliabilityState(t, s, model)
			response := resource.DeleteResponse{State: state}
			s.Delete(context.Background(), resource.DeleteRequest{State: state}, &response)
			if response.Diagnostics.HasError() || gets.Load() != int32(len(tc.sequence)) || deletes.Load() != 1 {
				t.Fatalf("premature deletion confirmation: GET=%d DELETE=%d %v", gets.Load(), deletes.Load(), response.Diagnostics)
			}
		})
	}
}

func TestDeleteVerificationRespectsDeadlineAndCancellation(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "retry after deadline", true: "cancellation"}[cancelled], func(t *testing.T) {
			var gets, deletes atomic.Int32
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "DELETE" {
					deletes.Add(1)
					_, _ = io.WriteString(w, `{"status":"ok"}`)
					return
				}
				gets.Add(1)
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(429)
				if cancelled {
					cancel()
				}
			}))
			defer api.Close()
			s := &serverResource{client: client.New(api.URL, ""), pollInterval: time.Millisecond}
			state := reliabilityState(t, s, reliabilityModel())
			response := resource.DeleteResponse{State: state}
			s.Delete(ctx, resource.DeleteRequest{State: state}, &response)
			if !response.Diagnostics.HasError() || gets.Load() != 1 || deletes.Load() != 1 {
				t.Fatalf("retry delay ignored cancellation/deadline: %v", response.Diagnostics)
			}
			assertServerID(t, response.State)
		})
	}
}

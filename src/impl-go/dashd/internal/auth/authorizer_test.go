// PD-G3: real RBAC dispatch through TokenAuthorizer + MTLSAuthorizer.
package auth

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// --- TokenAuthorizer --------------------------------------------------

func newTokenAuthorizer(t *testing.T) *TokenAuthorizer {
	t.Helper()
	rm := NewRoleMap()
	rm.Register(RPCInfo{Method: "/dashcenter.v1.ControlPlane/PutEni", Access: AccessWrite, AllowedRoles: []string{RoleOperator, RoleAdmin}})
	rm.Register(RPCInfo{Method: "/dashcenter.v1.ControlPlane/Get", Access: AccessRead, AllowedRoles: []string{RoleViewer, RoleOperator, RoleAdmin}})
	return &TokenAuthorizer{
		Tokens: map[string]Subject{
			"viewer-tok":   {Name: "alice", Role: RoleViewer},
			"operator-tok": {Name: "bob", Role: RoleOperator},
			"admin-tok":    {Name: "carol", Role: RoleAdmin},
		},
		Roles: rm,
	}
}

func ctxWithBearer(token string) context.Context {
	md := metadata.New(map[string]string{"authorization": "Bearer " + token})
	return metadata.NewIncomingContext(context.Background(), md)
}

func TestTokenAuthorizer_NoToken_Unauthenticated(t *testing.T) {
	a := newTokenAuthorizer(t)
	_, err := a.Authorize(context.Background(), "/dashcenter.v1.ControlPlane/Get")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("got %v; want ErrUnauthenticated", err)
	}
}

func TestTokenAuthorizer_BadToken_Unauthenticated(t *testing.T) {
	a := newTokenAuthorizer(t)
	_, err := a.Authorize(ctxWithBearer("not-a-token"), "/dashcenter.v1.ControlPlane/Get")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("got %v; want ErrUnauthenticated", err)
	}
}

func TestTokenAuthorizer_ViewerCannotWrite_PD_G3(t *testing.T) {
	a := newTokenAuthorizer(t)
	subj, err := a.Authorize(ctxWithBearer("viewer-tok"), "/dashcenter.v1.ControlPlane/PutEni")
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("got %v; want ErrPermissionDenied", err)
	}
	if subj.Name != "alice" {
		t.Errorf("expected verified subject alice in denial; got %q", subj.Name)
	}
}

func TestTokenAuthorizer_ViewerCanRead_PD_G3(t *testing.T) {
	a := newTokenAuthorizer(t)
	subj, err := a.Authorize(ctxWithBearer("viewer-tok"), "/dashcenter.v1.ControlPlane/Get")
	if err != nil {
		t.Fatalf("viewer + read: %v", err)
	}
	if subj.Role != RoleViewer {
		t.Errorf("subject role=%q; want viewer", subj.Role)
	}
}

func TestTokenAuthorizer_OperatorCanWrite_PD_G3(t *testing.T) {
	a := newTokenAuthorizer(t)
	if _, err := a.Authorize(ctxWithBearer("operator-tok"), "/dashcenter.v1.ControlPlane/PutEni"); err != nil {
		t.Errorf("operator + write: %v", err)
	}
}

func TestTokenAuthorizer_AdminImplicitlyAllowed(t *testing.T) {
	a := newTokenAuthorizer(t)
	// Admin is allowed even on unregistered methods.
	if _, err := a.Authorize(ctxWithBearer("admin-tok"), "/dashcenter.v1.UnknownService/SomeMethod"); err != nil {
		t.Errorf("admin on unregistered: %v", err)
	}
}

func TestTokenAuthorizer_GRPCCodeMapping(t *testing.T) {
	a := newTokenAuthorizer(t)
	_, err := a.Authorize(context.Background(), "/dashcenter.v1.ControlPlane/Get")
	if status.Code(err) != codes.Unauthenticated {
		t.Errorf("grpc code = %s; want Unauthenticated", status.Code(err))
	}
	_, err = a.Authorize(ctxWithBearer("viewer-tok"), "/dashcenter.v1.ControlPlane/PutEni")
	if status.Code(err) != codes.PermissionDenied {
		t.Errorf("grpc code = %s; want PermissionDenied", status.Code(err))
	}
}

// --- MTLSAuthorizer --------------------------------------------------

func TestMTLSAuthorizer_Smoke(t *testing.T) {
	// This test exercises the role lookup path via the ctx key shortcut.
	// The full TLS handshake is exercised by the live e2e against the
	// running dashd with self-signed certs.
	rm := NewRoleMap()
	rm.Register(RPCInfo{Method: "/x", Access: AccessRead, AllowedRoles: []string{RoleViewer}})
	a := &MTLSAuthorizer{
		CNRoles: map[string]Subject{"client-A": {Name: "client-A", Role: RoleViewer}},
		Roles:   rm,
	}
	// Inject CN via the test-only context key.
	ctx := context.WithValue(context.Background(), clientCNKey{}, "client-A")
	subj, err := a.Authorize(ctx, "/x")
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if subj.Name != "client-A" {
		t.Errorf("subject = %q; want client-A", subj.Name)
	}
}

func TestMTLSAuthorizer_UnknownCN_PermissionDenied(t *testing.T) {
	rm := NewRoleMap()
	rm.Register(RPCInfo{Method: "/x", Access: AccessRead, AllowedRoles: []string{RoleViewer}})
	a := &MTLSAuthorizer{CNRoles: map[string]Subject{}, Roles: rm}
	ctx := context.WithValue(context.Background(), clientCNKey{}, "outsider")
	_, err := a.Authorize(ctx, "/x")
	if !errors.Is(err, ErrPermissionDenied) {
		t.Errorf("got %v; want ErrPermissionDenied (D14: unmapped CN)", err)
	}
}

func TestMTLSAuthorizer_NoCN_Unauthenticated(t *testing.T) {
	a := &MTLSAuthorizer{Roles: NewRoleMap()}
	_, err := a.Authorize(context.Background(), "/x")
	if !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("got %v; want ErrUnauthenticated", err)
	}
}

func TestMTLSAuthorizer_KnownCNWrongRole(t *testing.T) {
	rm := NewRoleMap()
	rm.Register(RPCInfo{Method: "/write", Access: AccessWrite, AllowedRoles: []string{RoleOperator}})
	a := &MTLSAuthorizer{
		CNRoles: map[string]Subject{"viewer-cn": {Name: "viewer-cn", Role: RoleViewer}},
		Roles:   rm,
	}
	ctx := context.WithValue(context.Background(), clientCNKey{}, "viewer-cn")
	_, err := a.Authorize(ctx, "/write")
	if !errors.Is(err, ErrPermissionDenied) {
		t.Errorf("got %v; want ErrPermissionDenied", err)
	}
}

// --- RoleMap.AllowMethod tested in TestRoleMap_AllowMethod_ClosedDefault
// in auth_test.go; keeping the rest here keeps the file focused on PD wiring.

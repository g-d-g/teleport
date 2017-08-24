/*
Copyright 2015 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package auth

import (
	"context"
	"fmt"

	"github.com/gravitational/teleport"
	"github.com/gravitational/teleport/lib/services"

	"github.com/gravitational/trace"
)

// NewRoleAuthorizer authorizes everyone as predefined role
func NewRoleAuthorizer(r teleport.Role) (Authorizer, error) {
	authContext, err := contextForBuiltinRole(r)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return &contextAuthorizer{authContext: *authContext}, nil
}

// contextAuthorizer is a helper struct that always authorizes
// based on predefined context, helpful for tests
type contextAuthorizer struct {
	authContext AuthContext
}

// Authorize authorizes user based on identity supplied via context
func (r *contextAuthorizer) Authorize(ctx context.Context) (*AuthContext, error) {
	return &r.authContext, nil
}

// NewUserAuthorizer authorizes everyone as predefined local user
func NewUserAuthorizer(username string, identity services.Identity, access services.Access) (Authorizer, error) {
	authContext, err := contextForLocalUser(username, identity, access)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return &contextAuthorizer{authContext: *authContext}, nil
}

// NewAuthorizer returns new authorizer using backends
func NewAuthorizer(access services.Access, identity services.Identity, trust services.Trust) (Authorizer, error) {
	if access == nil {
		return nil, trace.BadParameter("missing parameter access")
	}
	if identity == nil {
		return nil, trace.BadParameter("missing parameter identity")
	}
	if trust == nil {
		return nil, trace.BadParameter("missing parameter trust")
	}
	return &authorizer{access: access, identity: identity, trust: trust}, nil
}

// Authorizer authorizes identity and returns auth context
type Authorizer interface {
	// Authorize authorizes user based on identity supplied via context
	Authorize(ctx context.Context) (*AuthContext, error)
}

// authorizer creates new local authorizer
type authorizer struct {
	access   services.Access
	identity services.Identity
	trust    services.Trust
}

// AuthzContext is authorization context
type AuthContext struct {
	// User is the user name
	User services.User
	// Checker is access checker
	Checker services.AccessChecker
}

// Authorize authorizes user based on identity supplied via context
func (a *authorizer) Authorize(ctx context.Context) (*AuthContext, error) {
	if ctx == nil {
		return nil, trace.AccessDenied("missing authentication context")
	}
	userI := ctx.Value(teleport.ContextUser)
	//fmt.Printf("Authorize: %#v\n", userI)
	switch user := userI.(type) {
	case teleport.LocalUser:
		return a.authorizeLocalUser(user)
	case teleport.RemoteUser:
		return a.authorizeRemoteUser(user)
	case teleport.BuiltinRole:
		return a.authorizeBuiltinRole(user)
	default:
		return nil, trace.AccessDenied("unsupported context type %T", userI)
	}
}

// authorizeLocalUser returns authz context based on the username
func (a *authorizer) authorizeLocalUser(u teleport.LocalUser) (*AuthContext, error) {
	return contextForLocalUser(u.Username, a.identity, a.access)
}

// authorizeRemoteUser returns checker based on cert authority roles
func (a *authorizer) authorizeRemoteUser(u teleport.RemoteUser) (*AuthContext, error) {
	ca, err := a.trust.GetCertAuthority(services.CertAuthID{Type: services.UserCA, DomainName: u.ClusterName}, false)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	roleNames, err := ca.CombinedMapping().Map(u.RemoteRoles)
	if err != nil {
		return nil, trace.AccessDenied("failed to map roles for remote user %v from cluster %v", u.Username, u.ClusterName)
	}
	checker, err := services.FetchRoles(roleNames, a.access, nil)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	user, err := services.NewUser(fmt.Sprintf("remote-%v-%v", u.Username, u.ClusterName))
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return &AuthContext{
		// this is done on purpose to make sure user does not match some real local user
		User:    user,
		Checker: checker,
	}, nil
}

// authorizeBuiltinRole authorizes builtin role
func (a *authorizer) authorizeBuiltinRole(r teleport.BuiltinRole) (*AuthContext, error) {
	return contextForBuiltinRole(r.Role)
}

// GetCheckerForBuiltinRole returns checkers for embedded builtin role
func GetCheckerForBuiltinRole(role teleport.Role) (services.AccessChecker, error) {
	switch role {
	case teleport.RoleAuth:
		return services.FromSpec(
			role.String(),
			services.RoleSpecV3{
				Allow: services.RoleConditions{
					Namespaces: []string{services.Wildcard},
					Rules: map[string][]string{
						services.KindAuthServer: services.RW(),
					},
				},
			})
	case teleport.RoleProvisionToken:
		return services.FromSpec(role.String(), services.RoleSpecV3{})
	case teleport.RoleNode:
		return services.FromSpec(
			role.String(),
			services.RoleSpecV3{
				Allow: services.RoleConditions{
					Namespaces: []string{services.Wildcard},
					Rules: map[string][]string{
						services.KindNode:          services.RW(),
						services.KindSession:       services.RW(),
						services.KindEvent:         services.RW(),
						services.KindProxy:         services.RO(),
						services.KindCertAuthority: services.RO(),
						services.KindUser:          services.RO(),
						services.KindNamespace:     services.RO(),
						services.KindRole:          services.RO(),
						services.KindAuthServer:    services.RO(),
						services.KindReverseTunnel: services.RO(),
					},
				},
			})
	case teleport.RoleProxy:
		return services.FromSpec(
			role.String(),
			services.RoleSpecV3{
				Allow: services.RoleConditions{
					Namespaces: []string{services.Wildcard},
					Rules: map[string][]string{
						services.KindProxy:                 services.RW(),
						services.KindOIDCRequest:           services.RW(),
						services.KindSession:               services.RW(),
						services.KindEvent:                 services.RW(),
						services.KindSAMLRequest:           services.RW(),
						services.KindOIDC:                  services.RO(),
						services.KindSAML:                  services.RO(),
						services.KindNamespace:             services.RO(),
						services.KindNode:                  services.RO(),
						services.KindAuthServer:            services.RO(),
						services.KindReverseTunnel:         services.RO(),
						services.KindCertAuthority:         services.RO(),
						services.KindUser:                  services.RO(),
						services.KindRole:                  services.RO(),
						services.KindClusterAuthPreference: services.RO(),
						services.KindClusterName:           services.RO(),
						services.KindStaticTokens:          services.RO(),
					},
				},
			})
	case teleport.RoleWeb:
		return services.FromSpec(
			role.String(),
			services.RoleSpecV3{
				Allow: services.RoleConditions{
					Namespaces: []string{services.Wildcard},
					Rules: map[string][]string{
						services.KindWebSession:     services.RW(),
						services.KindSession:        services.RW(),
						services.KindAuthServer:     services.RO(),
						services.KindUser:           services.RO(),
						services.KindRole:           services.RO(),
						services.KindNamespace:      services.RO(),
						services.KindTrustedCluster: services.RO(),
					},
				},
			})
	case teleport.RoleSignup:
		return services.FromSpec(
			role.String(),
			services.RoleSpecV3{
				Allow: services.RoleConditions{
					Namespaces: []string{services.Wildcard},
					Rules: map[string][]string{
						services.KindAuthServer:            services.RO(),
						services.KindClusterAuthPreference: services.RO(),
					},
				},
			})
	case teleport.RoleAdmin:
		return services.FromSpec(
			role.String(),
			services.RoleSpecV3{
				Options: services.RoleOptions{
					services.MaxSessionTTL: services.MaxDuration(),
				},
				Allow: services.RoleConditions{
					Namespaces: []string{services.Wildcard},
					Logins:     []string{},
					NodeLabels: map[string]string{services.Wildcard: services.Wildcard},
					Rules: map[string][]string{
						services.Wildcard: services.RW(),
					},
				},
			})
	case teleport.RoleNop:
		return services.FromSpec(
			role.String(),
			services.RoleSpecV3{
				Allow: services.RoleConditions{
					Namespaces: []string{},
					Rules:      map[string][]string{},
				},
			})
	}

	return nil, trace.NotFound("%v is not reconginzed", role.String())
}

func contextForBuiltinRole(r teleport.Role) (*AuthContext, error) {
	checker, err := GetCheckerForBuiltinRole(r)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	user, err := services.NewUser(fmt.Sprintf("builtin-%v", r))
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return &AuthContext{
		User:    user,
		Checker: checker,
	}, nil
}

func contextForLocalUser(username string, identity services.Identity, access services.Access) (*AuthContext, error) {
	user, err := identity.GetUser(username)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	checker, err := services.FetchRoles(user.GetRoles(), access, user.GetTraits())
	if err != nil {
		return nil, trace.Wrap(err)
	}
	return &AuthContext{
		User:    user,
		Checker: checker,
	}, nil
}

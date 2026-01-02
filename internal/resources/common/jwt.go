/*
Copyright 2026 GuionAI.

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

package common

import (
	supabasev1alpha1 "github.com/GuionAI/cloudnative-supabase/api/v1alpha1"
)

const (
	// DefaultJWTExpiration is the default JWT expiration in seconds (1 hour)
	DefaultJWTExpiration = 3600
)

// GetJWTExpiration returns the JWT expiration in seconds from the project spec
func GetJWTExpiration(project *supabasev1alpha1.SupabaseProject) int {
	if project.Spec.JWT != nil && project.Spec.JWT.ExpirationSeconds > 0 {
		return project.Spec.JWT.ExpirationSeconds
	}
	return DefaultJWTExpiration
}

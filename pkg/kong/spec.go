package kong

import (
	"strings"
	"time"
)

// HasKongFinalizer verify if the kong finalizer is set on the resource
func (d *Domain) HasKongFinalizer() bool {
	hasFinalizer := false
	for _, finalizer := range d.GetFinalizers() {
		if finalizer == Finalizer {
			hasFinalizer = true
			break
		}
	}
	return hasFinalizer
}

// IsMarkedForDeletion validates if the resource is set for deletion
func (d *Domain) IsMarkedForDeletion() bool {
	return d.DeletionTimestamp != nil || d.Status.DeletionTimestamp != nil
}

// IsPrimary validates if it's a primary domain
func (d *Domain) IsPrimary() bool {
	return len(d.Spec.Sub) == 0
}

// IsValidSharedDomain verifies if the shared domain it's a subdomain from the primary
func (d *Domain) IsValidSharedDomain() bool {
	return !d.IsPrimary() && d.IsValidDomain()
}

func (d *Domain) IsValidDomain() bool {
	if len(strings.Split(d.Spec.Sub, ".")) > 1 || len(strings.Split(d.Spec.PrimaryDomain, ".")) < 2 {
		return false
	}
	return true
}

func (d *Domain) GetDomain() string {
	if d.IsPrimary() {
		return d.GetPrimaryDomain()
	}
	return d.Spec.Sub + "." + d.Spec.PrimaryDomain
}

// GetDomainType returns the type of the resource: 'primary' or 'shared'
func (d *Domain) GetDomainType() string {
	if d.IsPrimary() {
		return "primary"
	}
	return "shared"
}

// GetPrimaryDomain returns the primary domain of the resource
func (d *Domain) GetPrimaryDomain() string {
	return d.Spec.PrimaryDomain
}

// IsUpdateExpired validates if the last update of the resource is expired
func (c *Domain) IsUpdateExpired(expireAfter time.Duration) bool {
	updatedAt := c.Status.LastUpdateTime.Add(expireAfter)
	if updatedAt.Before(time.Now().UTC()) {
		return true
	}
	return false
}

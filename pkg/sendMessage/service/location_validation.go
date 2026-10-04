package send_service

import (
	"fmt"
	"github.com/lucasgiovannibr/whatygo/pkg/apierror"
	"math"
)

// Validate checks a location request.
//
// The handler used to reject a latitude or a longitude equal to 0 as "required", so a
// place exactly on the equator or on the Greenwich meridian could not be sent. A JSON
// number cannot say "missing" apart from 0, so only the pair (0, 0) is treated as
// missing; the coordinates are also range-checked, which was not done at all.
func (l *LocationStruct) Validate() error {
	if math.IsNaN(l.Latitude) || math.IsNaN(l.Longitude) || math.IsInf(l.Latitude, 0) || math.IsInf(l.Longitude, 0) {
		return apierror.Invalid("latitude and longitude must be numbers")
	}
	if l.Latitude == 0 && l.Longitude == 0 {
		return apierror.Invalid("latitude and longitude are required")
	}
	if l.Latitude < -90 || l.Latitude > 90 {
		return apierror.Invalid(fmt.Sprintf("latitude must be between -90 and 90, got %v", l.Latitude))
	}
	if l.Longitude < -180 || l.Longitude > 180 {
		return apierror.Invalid(fmt.Sprintf("longitude must be between -180 and 180, got %v", l.Longitude))
	}
	if l.Address == "" {
		return apierror.Invalid("address is required")
	}
	if l.Name == "" {
		return apierror.Invalid("name is required")
	}
	return nil
}

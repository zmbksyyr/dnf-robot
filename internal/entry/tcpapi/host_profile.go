package tcpapi

import "robot/internal/foundation/hostprofile"

func SetHostProfile(profile hostprofile.Profile) { hostprofile.Configure(profile) }

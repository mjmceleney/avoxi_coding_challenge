package iplookup

import "strings"

// countryCodes is the set of country codes accepted in an allowed list: the officially
// assigned ISO 3166-1 alpha-2 codes, plus XK (Kosovo), which has no ISO code but is used
// by MaxMind.
var countryCodes = func() map[string]struct{} {
	codes := strings.Fields(isoCountryCodes)
	set := make(map[string]struct{}, len(codes)+1)
	for _, c := range codes {
		set[c] = struct{}{}
	}
	set["XK"] = struct{}{}
	return set
}()

// isoCountryCodes lists the officially assigned ISO 3166-1 alpha-2 codes.
const isoCountryCodes = `
AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ
BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ
CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ
DE DJ DK DM DO DZ
EC EE EG EH ER ES ET
FI FJ FK FM FO FR
GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY
HK HM HN HR HT HU
ID IE IL IM IN IO IQ IR IS IT
JE JM JO JP
KE KG KH KI KM KN KP KR KW KY KZ
LA LB LC LI LK LR LS LT LU LV LY
MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ
NA NC NE NF NG NI NL NO NP NR NU NZ
OM
PA PE PF PG PH PK PL PM PN PR PS PT PW PY
QA
RE RO RS RU RW
SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ
TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ
UA UG UM US UY UZ
VA VC VE VG VI VN VU
WF WS
YE YT
ZA ZM ZW
`

// validCountryCodes returns the entries of codes that are valid country codes, trimmed and
// upper-cased, and the entries that aren't, unchanged.
func validCountryCodes(codes []string) (valid, invalid []string) {
	valid = make([]string, 0, len(codes))
	for _, c := range codes {
		code := strings.ToUpper(strings.TrimSpace(c))
		if _, ok := countryCodes[code]; ok {
			valid = append(valid, code)
		} else {
			invalid = append(invalid, c)
		}
	}
	return valid, invalid
}

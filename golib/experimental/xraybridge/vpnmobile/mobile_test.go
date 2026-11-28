package vpnmobile

import "testing"

func TestValidateEndpoint(t *testing.T) {
	const link = "vless://12345678-1234-4234-8234-123456789abc@192.0.2.1:443?type=tcp&security=reality&encryption=none&sni=example.org&pbk=AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE&sid=abcd&fp=chrome"
	if err:=ValidateEndpoint(link,"192.0.2.1");err!=nil {t.Fatal(err)}
	for _,ip:=range []string{"192.0.2.2","example.org","0.0.0.0",""} {
		if err:=ValidateEndpoint(link,ip);err==nil {t.Fatal("invalid endpoint accepted")}
	}
}

func TestInvalidSnapshotPreservesExistingNetwork(t *testing.T) {
	defer SetNetworks("[]")
	if err:=SetNetworks(`[{"name":"Wi-Fi","handle":"123","v4":true}]`);err!=nil {t.Fatal(err)}
	before:=len(indexes)
	if err:=SetNetworks(`[{"name":"Wi-Fi","handle":"456","v4":true},{"name":"bad","handle":"0"}]`);err==nil {t.Fatal("invalid snapshot accepted")}
	if len(indexes)!=before || indexes["Wi-Fi [123]"]==0 {t.Fatal("invalid snapshot modified active networks")}
}

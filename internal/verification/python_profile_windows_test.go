//go:build windows

package verification

import ("os"; "path/filepath"; "testing")

func TestResolveExecutionProfileAddsExistingPythonSelfTest(t *testing.T) {
	root:=t.TempDir()
	if err:=os.MkdirAll(filepath.Join(root,"scripts"),0o755); err!=nil { t.Fatal(err) }
	if err:=os.WriteFile(filepath.Join(root,"scripts","self_test.py"),[]byte("print('ok')\n"),0o644); err!=nil { t.Fatal(err) }
	base:=Profile{ID:"python-standard",Commands:[]Command{{Name:`C:\\Python\\python.exe`,Args:[]string{"-m","unittest","discover","-v"},Cwd:"."}}}
	resolved,err:=resolveExecutionProfile(base,root,nil); if err!=nil { t.Fatal(err) }
	if len(resolved.Commands)!=2 || len(resolved.Commands[0].Args)!=1 || resolved.Commands[0].Args[0]!="scripts/self_test.py" { t.Fatalf("self-test was not prepended: %+v",resolved.Commands) }
	if len(base.Commands)!=1 { t.Fatal("declared profile was mutated") }
}
func TestResolveExecutionProfileKeepsFallbackWhenSelfTestAbsent(t *testing.T) {
	base:=Profile{ID:"python-standard",Commands:[]Command{{Name:"python.exe",Args:[]string{"-m","unittest","discover","-v"},Cwd:"."}}}
	resolved,err:=resolveExecutionProfile(base,t.TempDir(),nil); if err!=nil { t.Fatal(err) }
	if len(resolved.Commands)!=1 || resolved.Commands[0].Args[1]!="unittest" { t.Fatalf("fallback profile changed unexpectedly: %+v",resolved.Commands) }
}

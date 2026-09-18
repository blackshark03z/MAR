package verification

import ("slices"; "testing")

func testPythonPortableProfile() Profile {
	return Profile{ID:"python-portable", ChangedTests:ChangedTestPolicyPython, Commands:[]Command{{Name:"python.exe", Args:[]string{"-m","compileall","-q","."}, Cwd:"."}}}
}

func TestResolveChangedTestsSelectsSortedDeduplicatedPythonTests(t *testing.T) {
	resolved, err := testPythonPortableProfile().ResolveChangedTests([]string{"src/app.py","tests/z/test_z.py","tests/test_a.py","tests\\test_a.py","tests/helper.py","README.md"})
	if err != nil { t.Fatal(err) }
	want := [][]string{{"-m","unittest","discover","-s","tests","-p","test_a.py","-v"},{"-m","unittest","discover","-s","tests/z","-p","test_z.py","-v"},{"-m","compileall","-q","."}}
	if len(resolved.Commands)!=len(want) { t.Fatalf("unexpected command count: %+v", resolved.Commands) }
	for i,c := range resolved.Commands { if !slices.Equal(c.Args,want[i]) { t.Fatalf("command %d mismatch: %v",i,c.Args) } }
}
func TestResolveChangedTestsCompileallOnlyWhenNoChangedTests(t *testing.T) {
	resolved,err:=testPythonPortableProfile().ResolveChangedTests([]string{"src/app.py","tests/helper.py","docs/notes.md"}); if err!=nil{t.Fatal(err)}
	if len(resolved.Commands)!=1 || !slices.Equal(resolved.Commands[0].Args,[]string{"-m","compileall","-q","."}) { t.Fatalf("expected compileall-only: %+v",resolved.Commands) }
}
func TestResolveChangedTestsRejectsUnsafePaths(t *testing.T) {
	for _,p:=range []string{"../tests/test_escape.py","/tests/test_absolute.py","C:\\tests\\test_drive.py"} { if _,err:=testPythonPortableProfile().ResolveChangedTests([]string{p}); err==nil { t.Fatalf("unsafe path admitted: %q",p) } }
}
func TestResolvedChangedTestPlanHashIncludesSelectedTests(t *testing.T) {
	a,_:=testPythonPortableProfile().ResolveChangedTests([]string{"tests/test_a.py"}); b,_:=testPythonPortableProfile().ResolveChangedTests([]string{"tests/test_b.py"}); ha,_:=a.Hash(); hb,_:=b.Hash(); if ha==hb { t.Fatalf("different plans shared hash %s",ha) }
}

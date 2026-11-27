package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestCollectionTypesAcrossCachedAndBuiltPaths(t *testing.T) {
	bin := buildGeblangBinary(t, false)
	dir := t.TempDir()
	files := map[string]string{
		"geblang.yaml": "name: collectiontypes\nversion: 0.0.1\n",
		"impostor.gb":  `module impostor; export interface Animal { func name(): string; } export class Dog implements Animal { func name(): string { return "impostor"; } }`,
		"facade.gb":    `module facade; import donor as d; export type Animal = d.Animal;`,
		"donor.gb": `module donor;
export interface Animal { func name(): string; }
export interface Pet extends Animal { func age(): int; }
export enum Hound implements Pet { Dog; func name(): string { return "hound"; } func age(): int { return 1; } }
export class Failure extends Error { func Failure() { parent("fail"); } }
func skip(func next): func { return func(any values): void {}; }
@skip
export func decorated(list<int> values): void {}
export class Dog implements Animal { func name(): string { return "dog"; } }
export class Cat implements Animal { func name(): string { return "cat"; } }
export class Base {
    list<Dog> dogs = [];
    static list<Dog> shared = [];
    func accept(list<Animal> animals): void { animals.push(Dog()); }
    func values(): list<Animal> { return this.dogs; }
}
export func accept(list<Animal> animals): void { animals.push(Dog()); }
`,
		"app.gb": `module app;
import donor as d;
import impostor as other;
import facade;
import io;
func acceptNames(list<facade.Animal> values): void {}
func rejectNames(list<other.Animal> values): void {}
class Child extends d.Base {
    func own(list<d.Animal> animals): void { this.accept(animals); }
}
export func main(list<string> args): int {
    let raw = [1];
    d.decorated(raw);
    try { any alias = raw; alias.push("mixed"); }
    catch (TypeError error) { io.println("decorator"); }
    list<d.Animal> enums = [d.Hound.Dog];
    enums.push(d.Hound.Dog);
    list<d.Hound> ownEnums = [d.Hound.Dog];
    acceptNames(ownEnums);
    try { rejectNames(ownEnums); } catch (TypeError error) { io.println("enum identity"); }
    try { any alias = enums; alias.push(other.Dog()); } catch (TypeError error) { io.println("class identity"); }
    list<d.Failure> failures = [];
    failures.push(d.Failure());
    io.println("${enums.length()} ${failures.length()}");
    let child = Child();
    list<d.Animal> animals = child.values();
    d.accept(animals);
    child.own(animals);
    child.accept(animals);
    for (operation in [func(): void { animals.push(d.Cat()); },
        func(): void { any alias = child.dogs; alias[0] = d.Cat(); }]) {
        try { operation(); io.println("unchecked"); }
        catch (TypeError error) { io.println("rejected"); }
    }
    list<d.Animal> shared = Child.shared;
    try { shared.push(d.Cat()); } catch (TypeError error) { io.println("static"); }
    let inner = [1];
    list<list<int>> outer = [inner];
    try { any alias = inner; alias.push("wrong"); }
    catch (TypeError error) { io.println("nested"); }
    io.println("${animals.length()} ${outer.length()}");
    return 0;
}
`,
	}
	for name, source := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run := func(path string, args ...string) string {
		t.Helper()
		cmd := exec.Command(path, args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("run %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	want := "decorator\nenum identity\nclass identity\n2 1\nrejected\nrejected\nstatic\nnested\n3 1\n"
	for _, check := range []struct {
		name string
		args []string
	}{
		{name: "cold VM", args: []string{"app.gb"}},
		{name: "cached VM", args: []string{"app.gb"}},
		{name: "evaluator", args: []string{"--disable-vm", "app.gb"}},
	} {
		if got := run(bin, check.args...); got != want {
			t.Fatalf("%s: got %q, want %q", check.name, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".geblang-cache")); err != nil {
		t.Fatalf("bytecode cache: %v", err)
	}
	out := filepath.Join(dir, "collectiontypes")
	build := exec.Command(bin, "build", "--entry", "app", "--out", out, dir)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	for _, name := range []string{"impostor.gb", "facade.gb"} {
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Remove(filepath.Join(dir, "donor.gb")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "app.gb")); err != nil {
		t.Fatal(err)
	}
	if got := run(out); got != want {
		t.Fatalf("built binary: got %q, want %q", got, want)
	}
}

package provider

import (
	"testing"
)

func TestRewriteHeapMemory(t *testing.T) {
	for _, tc := range []struct{ name, command, want string }{
		{"generated", "java -Xms1000M -Xmx12000M -jar paper.jar nogui", "java -Xms4000M -Xmx20000M -jar paper.jar nogui"},
		{"neoforge", "/usr/lib/jvm/java/bin/java @user_jvm_args.txt -Xmx12G @libraries/unix_args.txt nogui", "/usr/lib/jvm/java/bin/java @user_jvm_args.txt -Xms4000M -Xmx20000M @libraries/unix_args.txt nogui"},
		{"quotes", `'/path with spaces/java'  '-Dmessage=a "quote" # tag' "-Xms1G" -Xmx12G -jar 'a b.jar' ''`, `'/path with spaces/java'  '-Dmessage=a "quote" # tag' -Xms4000M -Xmx20000M -jar 'a b.jar' ''`},
		{"application flags", "java -Xms1G -Xmx12G -jar app.jar -Xmx2G", "java -Xms4000M -Xmx20000M -jar app.jar -Xmx2G"},
		{"insert before jar", "java -Dfoo=bar -jar app.jar", "java -Dfoo=bar -Xms4000M -Xmx20000M -jar app.jar"},
		{"classpath", "java -cp 'a b' com.example.Main -Xmx2G", "java -cp 'a b' -Xms4000M -Xmx20000M com.example.Main -Xmx2G"},
		{"duplicates", "java -Xmx1G -Xms1G -Xmx12G -jar app.jar", "java -Xmx20000M -Xms4000M -Xmx20000M -jar app.jar"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := rewriteHeapMemory(tc.command, 4, 20)
			if err != nil || got != tc.want {
				t.Fatalf("got %q, %v; want %q", got, err, tc.want)
			}
			min, max, err := heapMemory(got)
			if err != nil || min == nil || max == nil || *min != 4 || *max != 20 {
				t.Fatalf("heap values did not round trip: %v %v %v", min, max, err)
			}
		})
	}
}

func TestHeapMemoryUnitsAndLastFlag(t *testing.T) {
	for _, tc := range []struct {
		flag string
		want float64
	}{
		{"-Xmx12G", 12.288}, {"-Xmx20000m", 20}, {"-Xmx20480000k", 20}, {"-Xmx20971520000", 20},
	} {
		got, err := heapUnits(tc.flag)
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %v, %v; want %v", tc.flag, got, err, tc.want)
		}
	}
	_, max, err := heapMemory("java -Xmx1G -Xmx12G -jar app.jar -Xmx2G")
	if err != nil || max == nil || *max != 12.288 {
		t.Fatalf("did not read last JVM option: %v %v", max, err)
	}
}

func TestManagedMemoryRejectsUnsafeCommands(t *testing.T) {
	for _, command := range []string{"sh start.sh", "java @args.txt", `java "unterminated`, "java -Xmxwrong -jar app.jar", "java -XX:MaxHeapSize=1234 -jar app.jar", "java -cp", "java -Xmx18446744073709551615G -jar app.jar"} {
		if _, err := rewriteHeapMemory(command, 4, 20); err == nil {
			t.Errorf("accepted unsupported command %q", command)
		}
	}
}

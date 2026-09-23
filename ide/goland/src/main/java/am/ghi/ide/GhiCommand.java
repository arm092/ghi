package am.ghi.ide;
import com.intellij.execution.configurations.GeneralCommandLine;
import com.intellij.util.execution.ParametersListUtil;
import java.nio.file.*;
import java.nio.charset.StandardCharsets;
import java.util.*;
final class GhiCommand {
    static Path directory(String base,String configured){
        if(base==null)throw new IllegalArgumentException("Open a project before running Ghi.");
        Path value=configured.isBlank()?Path.of(base):Path.of(configured);
        if(!value.isAbsolute())value=Path.of(base).resolve(value);
        return value.toAbsolutePath().normalize();
    }
    static String executable(String configured){
        if(!configured.isBlank())return configured;
        boolean windows=System.getProperty("os.name").startsWith("Windows");
        String local=System.getenv("LOCALAPPDATA");
        Path installed=windows && local!=null?Path.of(local,"Ghi","bin","ghi.exe"):
            Path.of(System.getProperty("user.home"),".local","bin","ghi");
        return Files.isRegularFile(installed)?installed.toString():windows?"ghi.exe":"ghi";
    }
    static GeneralCommandLine create(String executable,Path directory,String operation,String arguments){
        List<String> command=new ArrayList<>(List.of(executable,operation,directory.toString()));
        if(operation.equals("run") && !arguments.isBlank()){
            command.add("--");command.addAll(ParametersListUtil.parse(arguments));
        }
        return new GeneralCommandLine(command).withWorkDirectory(directory.toFile()).withCharset(StandardCharsets.UTF_8);
    }
}

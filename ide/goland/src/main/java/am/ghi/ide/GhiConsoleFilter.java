package am.ghi.ide;
import com.intellij.execution.filters.*;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.vfs.LocalFileSystem;
import java.nio.file.*;
final class GhiConsoleFilter implements Filter {
    private final Project project;
    private final Path directory;
    GhiConsoleFilter(Project project,Path directory){this.project=project;this.directory=directory;}
    public Result applyFilter(String line,int entireLength){
        var diagnostic=GhiDiagnostic.parse(line);
        if(diagnostic==null)return null;
        try{
            Path path=Path.of(diagnostic.path());
            if(!path.isAbsolute())path=directory.resolve(path);
            var file=LocalFileSystem.getInstance().findFileByPath(path.normalize().toString().replace('\\','/'));
            if(file==null)return null;
            int offset=entireLength-line.length();
            return new Result(offset+diagnostic.start(),offset+diagnostic.end(),
                new OpenFileHyperlinkInfo(project,file,diagnostic.line(),diagnostic.column()));
        }catch(InvalidPathException ignored){return null;}
    }
}

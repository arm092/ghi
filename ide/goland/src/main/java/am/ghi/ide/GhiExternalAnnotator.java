package am.ghi.ide;
import com.intellij.lang.annotation.*;
import com.intellij.openapi.editor.Editor;
import com.intellij.openapi.fileEditor.FileDocumentManager;
import com.intellij.openapi.util.TextRange;
import com.intellij.psi.PsiFile;
import com.intellij.execution.process.CapturingProcessHandler;
import java.nio.file.*;
import java.util.*;
import org.jetbrains.annotations.NotNull;
/** Runs the read-only compiler check in the external annotator background phase. */
public final class GhiExternalAnnotator extends ExternalAnnotator<GhiExternalAnnotator.Input,List<GhiExternalAnnotator.Problem>> {
    record Input(String executable,Path directory,Path file,String text,long stamp) {}
    record Problem(int line,int column,String message,long stamp) {}
    @Override public Input collectInformation(@NotNull PsiFile file,@NotNull Editor editor,boolean hasErrors){
        if(file.getVirtualFile()==null||!file.getVirtualFile().isInLocalFileSystem())return null;
        var document=editor.getDocument();
        // Check saved snapshots only: never silently save or annotate stale compiler output.
        if(FileDocumentManager.getInstance().isDocumentUnsaved(document))return null;
        var settings=GhiSettings.get(file.getProject()).getState();
        try{return new Input(GhiCommand.executable(settings.executable),GhiCommand.directory(file.getProject().getBasePath(),settings.directory),Path.of(file.getVirtualFile().getPath()).toAbsolutePath().normalize(),file.getText(),document.getModificationStamp());}
        catch(RuntimeException error){return null;}
    }
    @Override public List<Problem> doAnnotate(Input input){
        if(input==null)return List.of();
        try{
            var handler=new CapturingProcessHandler(GhiCommand.create(input.executable,input.directory,"check",""));
            var output=handler.runProcess(15000,true);
            if(output.isTimeout()||output.isCancelled())return List.of();
            return parse(output.getStdout()+"\n"+output.getStderr(),input);
        }catch(com.intellij.execution.ExecutionException error){return List.of();}
    }
    static List<Problem> parse(String output,Input input){
        List<Problem> problems=new ArrayList<>();
        for(String line:output.split("\\R")){
            var diagnostic=GhiDiagnostic.parse(line);if(diagnostic==null)continue;
            try{Path path=Path.of(diagnostic.path());if(!path.isAbsolute())path=input.directory.resolve(path);
                if(path.toAbsolutePath().normalize().equals(input.file))problems.add(new Problem(diagnostic.line(),diagnostic.column(),line.substring(diagnostic.end()+1).trim(),input.stamp));
            }catch(InvalidPathException ignored){}
        }
        return problems;
    }
    @Override public void apply(@NotNull PsiFile file,List<Problem> problems,@NotNull AnnotationHolder holder){
        var document=file.getViewProvider().getDocument();if(document==null||problems==null)return;
        for(Problem problem:problems){if(problem.stamp!=document.getModificationStamp()||problem.line>=document.getLineCount())continue;
            int start=Math.min(document.getLineStartOffset(problem.line)+problem.column,document.getLineEndOffset(problem.line));
            int end=Math.min(start+1,document.getTextLength());if(start==end&&start>0)start--;
            holder.newAnnotation(HighlightSeverity.ERROR,problem.message).range(new TextRange(start,end)).create();
        }
    }
}

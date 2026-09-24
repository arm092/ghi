package am.ghi.ide;

import com.intellij.openapi.fileEditor.FileDocumentManager;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.vfs.VirtualFile;
import com.intellij.xdebugger.breakpoints.*;
import org.jetbrains.annotations.NotNull;

/** Gutter breakpoints for Ghi sources; Delve verifies whether a line has code. */
public final class GhiBreakpointType extends XLineBreakpointType<GhiBreakpointType.Properties> {
    public static final class Properties extends XBreakpointProperties<Properties> {
        @Override public Properties getState(){return this;}
        @Override public void loadState(@NotNull Properties state){}
    }
    public GhiBreakpointType(){super("ghi-line","Ghi line");}
    @Override public Properties createBreakpointProperties(@NotNull VirtualFile file,int line){return new Properties();}
    @Override public boolean canPutAt(@NotNull VirtualFile file,int line,@NotNull Project project){
        if(file.getFileType()!=GhiFileType.INSTANCE||line<0)return false;
        var document=FileDocumentManager.getInstance().getDocument(file);
        if(document==null||line>=document.getLineCount())return false;
        String text=document.getText(new com.intellij.openapi.util.TextRange(document.getLineStartOffset(line),document.getLineEndOffset(line))).strip();
        return !text.isEmpty()&&!text.startsWith("//")&&!text.startsWith("namespace ")&&!text.startsWith("import ");
    }
}

package am.ghi.ide;
import com.intellij.openapi.fileTypes.SyntaxHighlighter;
import com.intellij.openapi.fileTypes.SyntaxHighlighterFactory;
import com.intellij.openapi.project.Project;
import com.intellij.openapi.vfs.VirtualFile;
public final class GhiHighlighterFactory extends SyntaxHighlighterFactory {
    public SyntaxHighlighter getSyntaxHighlighter(Project project,VirtualFile file){return new GhiHighlighter();}
}

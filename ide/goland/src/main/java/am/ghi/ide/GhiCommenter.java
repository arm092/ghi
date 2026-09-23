package am.ghi.ide;
import com.intellij.lang.Commenter;
public final class GhiCommenter implements Commenter {
    public String getLineCommentPrefix(){return "//";}
    public String getBlockCommentPrefix(){return "/*";}
    public String getBlockCommentSuffix(){return "*/";}
    public String getCommentedBlockCommentPrefix(){return null;}
    public String getCommentedBlockCommentSuffix(){return null;}
}

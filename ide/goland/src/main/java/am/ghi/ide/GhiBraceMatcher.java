package am.ghi.ide;
import com.intellij.lang.BracePair;
import com.intellij.lang.PairedBraceMatcher;
import com.intellij.psi.PsiFile;
import com.intellij.psi.tree.IElementType;
public final class GhiBraceMatcher implements PairedBraceMatcher {
    public BracePair[] getPairs(){return new BracePair[]{
        new BracePair(GhiLexer.LPAREN,GhiLexer.RPAREN,false),
        new BracePair(GhiLexer.LBRACKET,GhiLexer.RBRACKET,false),
        new BracePair(GhiLexer.LBRACE,GhiLexer.RBRACE,true)};}
    public boolean isPairedBracesAllowedBeforeType(IElementType left,IElementType context){return true;}
    public int getCodeConstructStart(PsiFile file,int offset){return offset;}
}

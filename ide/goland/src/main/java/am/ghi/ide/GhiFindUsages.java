package am.ghi.ide;
import com.intellij.lang.findUsages.FindUsagesProvider;
import com.intellij.lang.cacheBuilder.*;
import com.intellij.psi.*;
import com.intellij.psi.tree.TokenSet;
import org.jetbrains.annotations.NotNull;
public final class GhiFindUsages implements FindUsagesProvider {
    public WordsScanner getWordsScanner(){return new DefaultWordsScanner(new GhiLexer(),TokenSet.create(GhiLexer.IDENTIFIER),TokenSet.create(GhiLexer.COMMENT),TokenSet.create(GhiLexer.STRING));}
    public boolean canFindUsagesFor(@NotNull PsiElement element){return element instanceof GhiIdentifier;}
    public String getHelpId(@NotNull PsiElement element){return null;}
    public @NotNull String getType(@NotNull PsiElement element){return "Ghi symbol";}
    public @NotNull String getDescriptiveName(@NotNull PsiElement element){return element.getText();}
    public @NotNull String getNodeText(@NotNull PsiElement element,boolean full){return element.getText();}
}

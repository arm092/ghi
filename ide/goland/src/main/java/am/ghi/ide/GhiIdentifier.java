package am.ghi.ide;

import com.intellij.extapi.psi.ASTWrapperPsiElement;
import com.intellij.lang.ASTNode;
import com.intellij.openapi.util.TextRange;
import com.intellij.psi.*;
import com.intellij.psi.search.SearchScope;
import com.intellij.psi.search.GlobalSearchScope;
import org.jetbrains.annotations.NotNull;

/** Every source identifier has its own PSI node, including declarations. */
public final class GhiIdentifier extends ASTWrapperPsiElement implements PsiNameIdentifierOwner {
    public GhiIdentifier(ASTNode node) { super(node); }
    @Override public String getName() { return getText(); }
    @Override public PsiElement getNameIdentifier() { return this; }
    @Override public int getTextOffset() { return getTextRange().getStartOffset(); }
    @Override public @NotNull SearchScope getUseScope() { return GlobalSearchScope.projectScope(getProject()); }
    @Override public PsiElement setName(@NotNull String name) {
        if (!name.matches("[\\p{L}_][\\p{L}\\p{N}_]*")) throw new com.intellij.util.IncorrectOperationException("Invalid Ghi identifier");
        GhiLexer lexer=new GhiLexer();lexer.start(name);
        if(lexer.getTokenType()!=GhiLexer.IDENTIFIER||lexer.getTokenEnd()!=name.length())throw new com.intellij.util.IncorrectOperationException("Invalid Ghi identifier");
        PsiFile file = PsiFileFactory.getInstance(getProject()).createFileFromText("rename.ghi", GhiFileType.INSTANCE, name);
        return replace(file.getFirstChild());
    }
    @Override public PsiReference getReference() {
        return new PsiReferenceBase<GhiIdentifier>(this, TextRange.from(0, getTextLength())) {
            @Override public PsiElement resolve() { return GhiSymbols.forFile(getContainingFile()).resolve(getContainingFile(), getTextOffset()); }
            @Override public @NotNull Object[] getVariants() { return new Object[0]; }
            @Override public PsiElement handleElementRename(@NotNull String name) { return getElement().setName(name); }
        };
    }
}


package am.ghi.ide;
import com.intellij.lang.*;
import com.intellij.lexer.Lexer;
import com.intellij.openapi.project.Project;
import com.intellij.psi.*;
import com.intellij.extapi.psi.PsiFileBase;
import com.intellij.extapi.psi.ASTWrapperPsiElement;
import com.intellij.openapi.fileTypes.FileType;
import com.intellij.psi.tree.*;
public final class GhiParserDefinition implements ParserDefinition {
    static final IElementType NAME=new IElementType("NAME",GhiLanguage.INSTANCE);
    private static final IFileElementType FILE=new IFileElementType(GhiLanguage.INSTANCE);
    public Lexer createLexer(Project project){return new GhiLexer();}
    public PsiParser createParser(Project project){return (root,builder)->{
        var mark=builder.mark();while(!builder.eof()){if(builder.getTokenType()==GhiLexer.IDENTIFIER){var name=builder.mark();builder.advanceLexer();name.done(NAME);}else builder.advanceLexer();}mark.done(root);return builder.getTreeBuilt();
    };}
    public IFileElementType getFileNodeType(){return FILE;}
    public TokenSet getCommentTokens(){return TokenSet.create(GhiLexer.COMMENT);}
    public TokenSet getStringLiteralElements(){return TokenSet.create(GhiLexer.STRING);}
    public PsiElement createElement(ASTNode node){return node.getElementType()==NAME?new GhiIdentifier(node):new ASTWrapperPsiElement(node);}
    public PsiFile createFile(FileViewProvider provider){return new PsiFileBase(provider,GhiLanguage.INSTANCE){
        public FileType getFileType(){return GhiFileType.INSTANCE;}
    };}
}

package am.ghi.ide;
import com.intellij.lexer.Lexer;
import com.intellij.openapi.editor.DefaultLanguageHighlighterColors;
import com.intellij.openapi.editor.HighlighterColors;
import com.intellij.openapi.editor.colors.TextAttributesKey;
import com.intellij.openapi.fileTypes.SyntaxHighlighterBase;
import com.intellij.psi.TokenType;
import com.intellij.psi.tree.IElementType;
public final class GhiHighlighter extends SyntaxHighlighterBase {
    public Lexer getHighlightingLexer(){return new GhiLexer();}
    public TextAttributesKey[] getTokenHighlights(IElementType token){
        TextAttributesKey color=null;
        if(token==GhiLexer.KEYWORD)color=DefaultLanguageHighlighterColors.KEYWORD;
        else if(token==GhiLexer.STRING)color=DefaultLanguageHighlighterColors.STRING;
        else if(token==GhiLexer.COMMENT)color=DefaultLanguageHighlighterColors.BLOCK_COMMENT;
        else if(token==GhiLexer.NUMBER)color=DefaultLanguageHighlighterColors.NUMBER;
        else if(token==GhiLexer.OPERATOR)color=DefaultLanguageHighlighterColors.OPERATION_SIGN;
        else if(token==TokenType.BAD_CHARACTER)color=HighlighterColors.BAD_CHARACTER;
        return color==null?TextAttributesKey.EMPTY_ARRAY:new TextAttributesKey[]{color};
    }
}

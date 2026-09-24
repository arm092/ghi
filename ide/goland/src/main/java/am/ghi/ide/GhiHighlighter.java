package am.ghi.ide;
import com.intellij.lexer.Lexer;
import com.intellij.openapi.editor.HighlighterColors;
import com.intellij.openapi.editor.colors.TextAttributesKey;
import com.intellij.openapi.fileTypes.SyntaxHighlighterBase;
import com.intellij.psi.TokenType;
import com.intellij.psi.tree.IElementType;
public final class GhiHighlighter extends SyntaxHighlighterBase {
    public Lexer getHighlightingLexer(){return new GhiHighlightingLexer();}
    public TextAttributesKey[] getTokenHighlights(IElementType token){
        TextAttributesKey color=null;
        if(token==GhiLexer.KEYWORD)color=GhiColors.KEYWORD;
        else if(token==GhiLexer.STRING)color=GhiColors.STRING;
        else if(token==GhiLexer.COMMENT)color=GhiColors.COMMENT;
        else if(token==GhiLexer.NUMBER)color=GhiColors.NUMBER;
        else if(token==GhiLexer.OPERATOR)color=GhiColors.OPERATOR;
        else if(token==GhiHighlightingLexer.TYPE)color=GhiColors.TYPE;
        else if(token==GhiHighlightingLexer.INTERFACE)color=GhiColors.INTERFACE;
        else if(token==GhiHighlightingLexer.BUILTIN_TYPE)color=GhiColors.BUILTIN_TYPE;
        else if(token==GhiHighlightingLexer.FUNCTION)color=GhiColors.FUNCTION;
        else if(token==GhiHighlightingLexer.METHOD)color=GhiColors.METHOD;
        else if(token==GhiHighlightingLexer.PARAMETER)color=GhiColors.PARAMETER;
        else if(token==GhiHighlightingLexer.FIELD)color=GhiColors.FIELD;
        else if(token==GhiHighlightingLexer.NAMESPACE)color=GhiColors.NAMESPACE;
        else if(token==GhiHighlightingLexer.CONSTANT)color=GhiColors.CONSTANT;
        else if(token==GhiHighlightingLexer.LOCAL)color=GhiColors.LOCAL;
        else if(token==TokenType.BAD_CHARACTER)color=HighlighterColors.BAD_CHARACTER;
        return color==null?TextAttributesKey.EMPTY_ARRAY:new TextAttributesKey[]{color};
    }
}

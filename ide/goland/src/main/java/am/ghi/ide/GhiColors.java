package am.ghi.ide;
import com.intellij.openapi.editor.DefaultLanguageHighlighterColors;
import com.intellij.openapi.editor.colors.TextAttributesKey;
public final class GhiColors {
    private GhiColors(){}
    private static TextAttributesKey key(String name,TextAttributesKey fallback){return TextAttributesKey.createTextAttributesKey("GHI_"+name,fallback);}
    public static final TextAttributesKey TYPE=key("TYPE",DefaultLanguageHighlighterColors.CLASS_NAME),
        INTERFACE=key("INTERFACE",DefaultLanguageHighlighterColors.INTERFACE_NAME),
        BUILTIN_TYPE=key("BUILTIN_TYPE",DefaultLanguageHighlighterColors.PREDEFINED_SYMBOL),
        FUNCTION=key("FUNCTION",DefaultLanguageHighlighterColors.FUNCTION_DECLARATION),
        METHOD=key("METHOD",DefaultLanguageHighlighterColors.INSTANCE_METHOD),
        PARAMETER=key("PARAMETER",DefaultLanguageHighlighterColors.PARAMETER),
        FIELD=key("FIELD",DefaultLanguageHighlighterColors.INSTANCE_FIELD),
        NAMESPACE=key("NAMESPACE",DefaultLanguageHighlighterColors.METADATA),
        CONSTANT=key("CONSTANT",DefaultLanguageHighlighterColors.CONSTANT),
        LOCAL=key("LOCAL",DefaultLanguageHighlighterColors.LOCAL_VARIABLE),
        KEYWORD=key("KEYWORD",DefaultLanguageHighlighterColors.KEYWORD),
        STRING=key("STRING",DefaultLanguageHighlighterColors.STRING),
        NUMBER=key("NUMBER",DefaultLanguageHighlighterColors.NUMBER),
        COMMENT=key("COMMENT",DefaultLanguageHighlighterColors.BLOCK_COMMENT),
        OPERATOR=key("OPERATOR",DefaultLanguageHighlighterColors.OPERATION_SIGN);
}

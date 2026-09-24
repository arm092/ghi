package am.ghi.ide;
import com.intellij.openapi.options.colors.*;
import com.intellij.openapi.fileTypes.SyntaxHighlighter;
import com.intellij.openapi.editor.colors.TextAttributesKey;
import javax.swing.Icon;
import java.util.Map;
public final class GhiColorSettingsPage implements ColorSettingsPage {
    public Icon getIcon(){return GhiIcons.FILE;}
    public String getDisplayName(){return "Ghi";}
    public SyntaxHighlighter getHighlighter(){return new GhiHighlighter();}
    public Map<String,TextAttributesKey> getAdditionalHighlightingTagToDescriptorMap(){return null;}
    public AttributesDescriptor[] getAttributeDescriptors(){return new AttributesDescriptor[]{
        new AttributesDescriptor("Types//Class and named type",GhiColors.TYPE),
        new AttributesDescriptor("Types//Interface",GhiColors.INTERFACE),
        new AttributesDescriptor("Types//Built-in type",GhiColors.BUILTIN_TYPE),
        new AttributesDescriptor("Functions//Function",GhiColors.FUNCTION),
        new AttributesDescriptor("Functions//Method",GhiColors.METHOD),
        new AttributesDescriptor("Variables//Parameter",GhiColors.PARAMETER),
        new AttributesDescriptor("Variables//Field",GhiColors.FIELD),
        new AttributesDescriptor("Variables//Local variable",GhiColors.LOCAL),
        new AttributesDescriptor("Variables//Constant",GhiColors.CONSTANT),
        new AttributesDescriptor("Namespace and import alias",GhiColors.NAMESPACE),
        new AttributesDescriptor("Keyword",GhiColors.KEYWORD),
        new AttributesDescriptor("String",GhiColors.STRING),
        new AttributesDescriptor("Number",GhiColors.NUMBER),
        new AttributesDescriptor("Comment",GhiColors.COMMENT),
        new AttributesDescriptor("Operator",GhiColors.OPERATOR)};}
    public ColorDescriptor[] getColorDescriptors(){return ColorDescriptor.EMPTY_ARRAY;}
    public String getDemoText(){return """
        namespace main
        import fmt "go:fmt"

        // Ghi: Go, Hierarchy, Interfaces
        interface Described { func describe() string }
        class Person implements Described {
            protected name string
            constructor(name string = "Guest") {
                this.name = name
            }
            public func describe() string { return this.name }
        }
        class Developer extends Person {
            public experience int
            constructor(name string, experience int = 1) {
                parent(name)
                this.experience = experience
            }
            public override func describe() string {
                return parent.describe() + " from Ghi"
            }
        }
        func introduce(person Person) {
            fmt.Println(person.describe())
        }
        func main() {
            developer := Developer("Arman", 2)
            introduce(developer)
        }
        """;}
}

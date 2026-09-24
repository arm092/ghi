package am.ghi.ide;
import com.intellij.openapi.fileTypes.LanguageFileType;
import javax.swing.Icon;
public final class GhiFileType extends LanguageFileType {
    public static final GhiFileType INSTANCE = new GhiFileType();
    private GhiFileType() { super(GhiLanguage.INSTANCE); }
    public String getName() { return "Ghi"; }
    public String getDescription() { return "Ghi source"; }
    public String getDefaultExtension() { return "ghi"; }
    public Icon getIcon() { return GhiIcons.FILE; }
}

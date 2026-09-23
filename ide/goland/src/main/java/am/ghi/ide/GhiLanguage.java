package am.ghi.ide;
import com.intellij.lang.Language;
public final class GhiLanguage extends Language {
    public static final GhiLanguage INSTANCE = new GhiLanguage();
    private GhiLanguage() { super("Ghi"); }
}
